package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"wifi-radar/internal/api"
	"wifi-radar/internal/collector"
	"wifi-radar/internal/model"
	"wifi-radar/internal/store"
)

type ifList []string

func (i *ifList) String() string {
	return fmt.Sprintf("%v", *i)
}

func (i *ifList) Set(value string) error {
	if value == "" {
		return fmt.Errorf("interface name cannot be empty")
	}
	*i = append(*i, value)
	return nil
}

func main() {
	var (
		ifs         ifList
		interval    time.Duration
		listen      string
		public      bool
		askIf       bool
		openBrowser bool
		mode        string
		targetSSID  string
		targetBSSID string
	)

	flag.Var(&ifs, "if", "interface name to monitor (repeatable)")
	flag.DurationVar(&interval, "interval", 500*time.Millisecond, "sampling interval")
	flag.StringVar(&listen, "listen", "127.0.0.1:8888", "HTTP bind address")
	flag.BoolVar(&public, "public", false, "bind 0.0.0.0 (overrides listen if set)")
	flag.BoolVar(&askIf, "ask-if", false, "always ask which interface to use")
	flag.BoolVar(&openBrowser, "open", true, "open Firefox after start")
	flag.StringVar(&mode, "mode", "scan", "collection mode: scan or link")
	flag.StringVar(&targetSSID, "ssid", "", "target SSID for scan mode")
	flag.StringVar(&targetBSSID, "bssid", "", "target BSSID for scan mode")
	flag.Parse()

	if len(ifs) == 0 {
		detected, err := listInterfaces()
		if err != nil {
			log.Fatalf("list interfaces: %v", err)
		}
		if len(detected) == 0 {
			log.Fatalf("no interfaces found; use --if <ifname>")
		}
		if len(detected) == 1 && !askIf {
			ifs = append(ifs, detected[0])
		} else {
			selected, err := promptInterface(detected)
			if err != nil {
				log.Fatalf("select interface: %v", err)
			}
			ifs = append(ifs, selected)
		}
	}

	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "scan"
	}
	if mode != "scan" && mode != "link" {
		log.Fatalf("invalid mode: %s (use scan or link)", mode)
	}
	if mode == "scan" && len(ifs) > 1 {
		log.Fatalf("scan mode supports a single interface; got %d", len(ifs))
	}

	if public {
		listen = "0.0.0.0:8888"
	}

	st := store.New(8)
	apiHandler := api.API{Store: st}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", apiHandler.Status)
	mux.HandleFunc("/api/best", apiHandler.Best)
	mux.HandleFunc("/api/stream", apiHandler.Stream)

	staticDir := filepath.Join(mustCwd(), "web", "static")
	mux.Handle("/", http.FileServer(http.Dir(staticDir)))

	collectors, err := buildCollectors(mode, []string(ifs), targetSSID, targetBSSID)
	if err != nil {
		log.Fatalf("setup collectors: %v", err)
	}
	go collectLoop(st, collectors, interval)

	log.Printf("listening on http://%s", listen)
	if openBrowser {
		go openFirefox(listen)
	}
	if err := http.ListenAndServe(listen, mux); err != nil {
		log.Fatal(err)
	}
}

func collectLoop(st *store.Store, collectors []namedSampler, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		for _, c := range collectors {
			sample, err := c.sampler.Collect()
			if err != nil {
				if errors.Is(err, collector.ErrNotConnected) {
					continue
				}
				if errors.Is(err, collector.ErrTargetNotFound) {
					st.Update(sample)
					continue
				}
				log.Printf("collect %s: %v", c.name, err)
				continue
			}
			st.Update(sample)
		}
		<-ticker.C
	}
}

func mustCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("get cwd: %v", err)
	}
	return cwd
}

type sampler interface {
	Collect() (model.Sample, error)
}

type namedSampler struct {
	name    string
	sampler sampler
}

func buildCollectors(mode string, ifs []string, targetSSID string, targetBSSID string) ([]namedSampler, error) {
	collectors := make([]namedSampler, 0, len(ifs))
	if mode == "scan" {
		target := collector.ScanTarget{
			SSID:  strings.TrimSpace(targetSSID),
			BSSID: strings.TrimSpace(targetBSSID),
		}
		target, useSudo, err := resolveScanTarget(ifs[0], target)
		if err != nil {
			return nil, err
		}
		scanner := &collector.ScanCollector{
			IfName:  ifs[0],
			Target:  target,
			UseSudo: useSudo,
		}
		collectors = append(collectors, namedSampler{
			name:    ifs[0],
			sampler: scanner,
		})
		return collectors, nil
	}

	for _, ifname := range ifs {
		collectors = append(collectors, namedSampler{
			name:    ifname,
			sampler: collector.Collector{IfName: ifname},
		})
	}
	return collectors, nil
}

func resolveScanTarget(ifname string, target collector.ScanTarget) (collector.ScanTarget, bool, error) {
	if target.SSID != "" || target.BSSID != "" {
		return target, false, nil
	}
	networks, useSudo, err := collector.ScanNetworksWithFallback(ifname, false)
	if err != nil {
		return collector.ScanTarget{}, useSudo, err
	}
	if len(networks) == 0 {
		return collector.ScanTarget{}, useSudo, errors.New("no networks found in scan results")
	}
	target, err = promptNetwork(networks)
	return target, useSudo, err
}

func promptNetwork(networks []model.Sample) (collector.ScanTarget, error) {
	if len(networks) == 0 {
		return collector.ScanTarget{}, errors.New("no networks to select")
	}
	sort.Slice(networks, func(i, j int) bool {
		if networks[i].SignalDBM == networks[j].SignalDBM {
			return networks[i].SSID < networks[j].SSID
		}
		return networks[i].SignalDBM > networks[j].SignalDBM
	})

	fmt.Println("Select network to track:")
	for i, n := range networks {
		ssid := n.SSID
		if ssid == "" {
			ssid = "<hidden>"
		}
		signal := "-"
		if n.SignalDBM != 0 {
			signal = fmt.Sprintf("%d dBm", n.SignalDBM)
		}
		freq := "-"
		if n.FreqMHz != 0 {
			freq = fmt.Sprintf("%d MHz", n.FreqMHz)
		}
		fmt.Printf("  %d) %s  %s  %s  %s\n", i+1, ssid, n.BSSID, signal, freq)
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("Enter number: ")
		text, err := reader.ReadString('\n')
		if err != nil {
			return collector.ScanTarget{}, err
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		var choice int
		if _, err := fmt.Sscanf(text, "%d", &choice); err != nil {
			fmt.Println("Invalid number.")
			continue
		}
		if choice < 1 || choice > len(networks) {
			fmt.Println("Out of range.")
			continue
		}
		selected := networks[choice-1]
		return collector.ScanTarget{
			SSID:  selected.SSID,
			BSSID: selected.BSSID,
		}, nil
	}
}

func listInterfaces() ([]string, error) {
	out, err := exec.Command("iw", "dev").Output()
	if err != nil {
		return nil, fmt.Errorf("iw dev: %w", err)
	}

	var ifs []string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "Interface ") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "Interface "))
			if name != "" {
				ifs = append(ifs, name)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan interfaces: %w", err)
	}
	return ifs, nil
}

func promptInterface(ifs []string) (string, error) {
	fmt.Println("Select interface:")
	for i, name := range ifs {
		fmt.Printf("  %d) %s\n", i+1, name)
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("Enter number: ")
		text, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		var choice int
		if _, err := fmt.Sscanf(text, "%d", &choice); err != nil {
			fmt.Println("Invalid number.")
			continue
		}
		if choice < 1 || choice > len(ifs) {
			fmt.Println("Out of range.")
			continue
		}
		return ifs[choice-1], nil
	}
}

func openFirefox(listen string) {
	time.Sleep(300 * time.Millisecond)
	url := fmt.Sprintf("http://%s/", listen)
	if err := exec.Command("firefox", url).Start(); err == nil {
		return
	}
	_ = exec.Command("xdg-open", url).Start()
}
