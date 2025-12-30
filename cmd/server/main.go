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
	"strings"
	"time"

	"wifi-radar/internal/api"
	"wifi-radar/internal/collector"
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
	)

	flag.Var(&ifs, "if", "interface name to monitor (repeatable)")
	flag.DurationVar(&interval, "interval", 500*time.Millisecond, "sampling interval")
	flag.StringVar(&listen, "listen", "127.0.0.1:8888", "HTTP bind address")
	flag.BoolVar(&public, "public", false, "bind 0.0.0.0 (overrides listen if set)")
	flag.BoolVar(&askIf, "ask-if", false, "always ask which interface to use")
	flag.BoolVar(&openBrowser, "open", true, "open Firefox after start")
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

	go collectLoop(st, ifs, interval)

	log.Printf("listening on http://%s", listen)
	if openBrowser {
		go openFirefox(listen)
	}
	if err := http.ListenAndServe(listen, mux); err != nil {
		log.Fatal(err)
	}
}

func collectLoop(st *store.Store, ifs []string, interval time.Duration) {
	collectors := make([]collector.Collector, 0, len(ifs))
	for _, ifname := range ifs {
		collectors = append(collectors, collector.Collector{IfName: ifname})
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		for _, c := range collectors {
			sample, err := c.Collect()
			if err != nil {
				if errors.Is(err, collector.ErrNotConnected) {
					continue
				}
				log.Printf("collect %s: %v", c.IfName, err)
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
