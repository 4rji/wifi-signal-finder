package serverapp

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
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

type availableFunction struct {
	name        string
	description string
	mode        string
	aliases     []string
}

var startupFunctions = []availableFunction{
	{
		name:        "scan",
		description: "Scan nearby Wi-Fi networks and track RSSI without connecting (default).",
		mode:        "scan",
	},
	{
		name:        "metrics",
		description: "Monitor the connected link with signal, RX Mbps, and TX Mbps (--mode link).",
		mode:        "link",
		aliases:     []string{"metric", "metrix", "link"},
	},
}

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

func Run(args []string) {
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

	selectedFunction, args, hasFunction := extractFunction(args)
	showedMenu := false

	flags := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	flags.Var(&ifs, "if", "interface name to monitor (repeatable)")
	flags.DurationVar(&interval, "interval", 500*time.Millisecond, "sampling interval")
	flags.StringVar(&listen, "listen", "0.0.0.0:8888", "HTTP bind address")
	flags.BoolVar(&public, "public", false, "bind 0.0.0.0 (overrides listen if set)")
	flags.BoolVar(&askIf, "ask-if", false, "always ask which interface to use")
	flags.BoolVar(&openBrowser, "open", true, "open Firefox after start")
	flags.StringVar(&mode, "mode", "scan", "collection mode: scan or link")
	flags.StringVar(&targetSSID, "ssid", "", "target SSID for scan mode")
	flags.StringVar(&targetBSSID, "bssid", "", "target BSSID for scan mode")
	flags.Usage = func() {
		printUsage(flags)
	}
	if err := flags.Parse(args); err != nil {
		log.Fatal(err)
	}

	if flags.NArg() > 0 {
		log.Fatalf("unknown function or argument: %s", flags.Arg(0))
	}

	if hasFunction {
		mode = selectedFunction.mode
	} else if !flagWasSet(flags, "mode") {
		var err error
		selectedFunction, err = promptFunction(startupFunctions)
		if err != nil {
			log.Fatalf("select function: %v", err)
		}
		showedMenu = true
		mode = selectedFunction.mode
	}

	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "scan"
	}
	if mode != "scan" && mode != "link" {
		log.Fatalf("invalid mode: %s (use scan or link)", mode)
	}

	if public {
		listen = "0.0.0.0:8888"
	}

	if selectedFunction.name == "" {
		selectedFunction = functionForMode(mode)
	}
	printStartupInfo(os.Stdout, selectedFunction.name, mode, listen, !showedMenu)

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

	if mode == "scan" && len(ifs) > 1 {
		log.Fatalf("scan mode supports a single interface; got %d", len(ifs))
	}

	st := store.New(8)
	apiHandler := api.API{Store: st}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", apiHandler.Status)
	mux.HandleFunc("/api/best", apiHandler.Best)
	mux.HandleFunc("/api/stream", apiHandler.Stream)

	staticDir := resolveStaticDir()
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

func printUsage(flags *flag.FlagSet) {
	out := flags.Output()
	fmt.Fprintf(out, "Usage: %s [function] [options]\n\n", os.Args[0])
	printAvailableFunctions(out)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Examples:")
	fmt.Fprintf(out, "  %s\n", os.Args[0])
	fmt.Fprintf(out, "  %s scan\n", os.Args[0])
	fmt.Fprintf(out, "  %s metrics --if wlp0s20f3\n", os.Args[0])
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Options:")
	flags.PrintDefaults()
}

func printStartupInfo(w io.Writer, function string, mode string, listen string, includeFunctions bool) {
	if includeFunctions {
		fmt.Fprintln(w, "WiFi Radar")
		printAvailableFunctions(w)
	}
	if function != "" {
		fmt.Fprintf(w, "Selected function: %s\n", function)
	}
	fmt.Fprintf(w, "Selected mode: %s\n", mode)
	fmt.Fprintf(w, "Listening: http://%s/\n", listen)
	fmt.Fprintln(w, "Use --help to see all flags.")
	fmt.Fprintln(w)
}

func printAvailableFunctions(w io.Writer) {
	fmt.Fprintln(w, "Available functions:")
	for _, fn := range startupFunctions {
		fmt.Fprintf(w, "  %-13s %s\n", fn.name, fn.description)
	}
}

func extractFunction(args []string) (availableFunction, []string, bool) {
	for i, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		fn, ok := findFunction(arg)
		if !ok {
			return availableFunction{}, args, false
		}
		remaining := make([]string, 0, len(args)-1)
		remaining = append(remaining, args[:i]...)
		remaining = append(remaining, args[i+1:]...)
		return fn, remaining, true
	}
	return availableFunction{}, args, false
}

func findFunction(name string) (availableFunction, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, fn := range startupFunctions {
		if name == fn.name {
			return fn, true
		}
		for _, alias := range fn.aliases {
			if name == alias {
				return fn, true
			}
		}
	}
	return availableFunction{}, false
}

func functionForMode(mode string) availableFunction {
	for _, fn := range startupFunctions {
		if fn.mode == mode {
			return fn
		}
	}
	return availableFunction{}
}

func flagWasSet(flags *flag.FlagSet, name string) bool {
	wasSet := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == name {
			wasSet = true
		}
	})
	return wasSet
}

func promptFunction(functions []availableFunction) (availableFunction, error) {
	if len(functions) == 0 {
		return availableFunction{}, errors.New("no functions available")
	}

	fmt.Println("WiFi Radar")
	printAvailableFunctions(os.Stdout)
	fmt.Println()
	fmt.Println("Select function:")
	for i, fn := range functions {
		fmt.Printf("  %d) %s\n", i+1, fn.name)
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("Enter number or name: ")
		text, err := reader.ReadString('\n')
		if err != nil {
			return availableFunction{}, err
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if fn, ok := findFunction(text); ok {
			return fn, nil
		}
		var choice int
		if _, err := fmt.Sscanf(text, "%d", &choice); err != nil {
			fmt.Println("Invalid function.")
			continue
		}
		if choice < 1 || choice > len(functions) {
			fmt.Println("Out of range.")
			continue
		}
		return functions[choice-1], nil
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

func resolveStaticDir() string {
	if env := strings.TrimSpace(os.Getenv("WIFI_RADAR_STATIC_DIR")); env != "" {
		if dirExists(env) {
			return env
		}
		log.Fatalf("static dir not found in WIFI_RADAR_STATIC_DIR: %s", env)
	}

	cwd := mustCwd()
	if dirExists(filepath.Join(cwd, "web", "static")) {
		return filepath.Join(cwd, "web", "static")
	}

	exe, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exe)
		if dirExists(filepath.Join(exeDir, "web", "static")) {
			return filepath.Join(exeDir, "web", "static")
		}
		if dirExists(filepath.Join(exeDir, "..", "web", "static")) {
			return filepath.Join(exeDir, "..", "web", "static")
		}
	}

	log.Fatalf("static assets not found; set WIFI_RADAR_STATIC_DIR to the web/static folder")
	return ""
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
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
