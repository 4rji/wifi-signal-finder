package serverapp

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
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
	webassets "wifi-radar/web"
)

type ifList []string

type availableFunction struct {
	name        string
	description string
	mode        string
	example     string
	aliases     []string
}

var startupFunctions = []availableFunction{
	{
		name:        "scan",
		description: "Scan nearby Wi-Fi networks and track RSSI without connecting (default).",
		mode:        "scan",
		example:     `go run . scan -i wlp0s20f3 --ssid "MyWiFi"`,
	},
	{
		name:        "metrics",
		description: "Monitor the connected link with signal, RX Mbps, and TX Mbps (--mode link).",
		mode:        "link",
		example:     "go run . metrics -i wlp0s20f3",
		aliases:     []string{"metric", "metrix", "link"},
	},
}

const (
	ansiReset   = "\033[0m"
	ansiBold    = "\033[1m"
	ansiDim     = "\033[2m"
	ansiRed     = "\033[31m"
	ansiGreen   = "\033[32m"
	ansiYellow  = "\033[33m"
	ansiBlue    = "\033[34m"
	ansiMagenta = "\033[35m"
	ansiCyan    = "\033[36m"
)

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
		raspberryUI bool
		mode        string
		targetSSID  string
		targetBSSID string
	)

	selectedFunction, args, hasFunction := extractFunction(args)
	showedMenu := false

	flags := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	flags.Var(&ifs, "i", "interface name to monitor (repeatable)")
	flags.DurationVar(&interval, "interval", 500*time.Millisecond, "sampling interval")
	flags.StringVar(&listen, "listen", "0.0.0.0:8888", "HTTP bind address")
	flags.BoolVar(&public, "public", false, "bind 0.0.0.0 (overrides listen if set)")
	flags.BoolVar(&askIf, "ask-if", false, "always ask which interface to use")
	flags.BoolVar(&openBrowser, "open", true, "open browser after start")
	flags.BoolVar(&raspberryUI, "rb", false, "serve compact Raspberry Pi 2.4 inch display UI")
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
	} else if !flagWasSet(flags, "mode") && !raspberryUI {
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
	printStartupInfo(os.Stdout, selectedFunction.name, mode, listen, !showedMenu, raspberryUI)

	if len(ifs) == 0 {
		detected, err := listInterfaces()
		if err != nil {
			log.Fatalf("list interfaces: %v", err)
		}
		if len(detected) == 0 {
			log.Fatalf("no interfaces found; use -i <ifname>")
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

	mux.Handle("/", staticHandler(resolveStaticFileSystem(), raspberryUI))

	collectors, err := buildCollectors(mode, []string(ifs), targetSSID, targetBSSID)
	if err != nil {
		log.Fatalf("setup collectors: %v", err)
	}
	go collectLoop(st, collectors, interval)

	log.Printf("listening on http://%s", listen)
	if openBrowser {
		go openBrowserForDisplay(listen, raspberryUI)
	}
	if err := http.ListenAndServe(listen, mux); err != nil {
		log.Fatal(err)
	}
}

func printUsage(flags *flag.FlagSet) {
	out := flags.Output()
	fmt.Fprintf(out, "%s go run . [function] [options]\n\n", paint("Usage:", ansiBold, ansiCyan))
	printAvailableFunctions(out)
	fmt.Fprintln(out)
	fmt.Fprintln(out, paint("Examples:", ansiBold, ansiYellow))
	fmt.Fprintf(out, "  %s\n", paint("go run .", ansiBlue))
	for _, fn := range startupFunctions {
		fmt.Fprintf(out, "  %s\n", paint(fn.example, ansiBlue))
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, paint("Options:", ansiBold, ansiYellow))
	flags.PrintDefaults()
}

func printStartupInfo(w io.Writer, function string, mode string, listen string, includeFunctions bool, raspberryUI bool) {
	if includeFunctions {
		fmt.Fprintln(w, paint("WiFi Radar", ansiBold, ansiCyan))
		printAvailableFunctions(w)
	}
	if function != "" {
		fmt.Fprintf(w, "%s %s\n", paint("Selected function:", ansiBold, ansiYellow), paint(function, ansiGreen, ansiBold))
	}
	fmt.Fprintf(w, "%s %s\n", paint("Selected mode:", ansiBold, ansiYellow), paint(mode, ansiGreen))
	if raspberryUI {
		fmt.Fprintf(w, "%s %s\n", paint("Display UI:", ansiBold, ansiYellow), paint("Raspberry Pi 2.4 inch", ansiGreen))
	}
	fmt.Fprintf(w, "%s %s\n", paint("Listening:", ansiBold, ansiYellow), paint(fmt.Sprintf("http://%s/", listen), ansiBlue))
	fmt.Fprintln(w, paint("Use --help to see all flags.", ansiDim))
	fmt.Fprintln(w)
}

func printAvailableFunctions(w io.Writer) {
	fmt.Fprintln(w, paint("Available functions:", ansiBold, ansiYellow))
	for _, fn := range startupFunctions {
		fmt.Fprintf(w, "  %s %s\n", paint(fmt.Sprintf("%-13s", fn.name), ansiGreen, ansiBold), paint(fn.description, ansiDim))
		fmt.Fprintf(w, "  %s %s\n", paint("command:", ansiYellow), paint(fn.example, ansiBlue))
	}
}

func paint(text string, styles ...string) string {
	if colorDisabled() {
		return text
	}
	return strings.Join(styles, "") + text + ansiReset
}

func colorDisabled() bool {
	return os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb"
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

	fmt.Println(paint("WiFi Radar", ansiBold, ansiCyan))
	printAvailableFunctions(os.Stdout)
	fmt.Println()
	fmt.Println(paint("Select function:", ansiBold, ansiYellow))
	for i, fn := range functions {
		fmt.Printf("  %s %s\n", paint(fmt.Sprintf("%d)", i+1), ansiYellow, ansiBold), paint(fn.name, ansiGreen, ansiBold))
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print(paint("Enter number or name: ", ansiMagenta, ansiBold))
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
			fmt.Println(paint("Invalid function.", ansiRed, ansiBold))
			continue
		}
		if choice < 1 || choice > len(functions) {
			fmt.Println(paint("Out of range.", ansiRed, ansiBold))
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

func resolveStaticFileSystem() http.FileSystem {
	if env := strings.TrimSpace(os.Getenv("WIFI_RADAR_STATIC_DIR")); env != "" {
		if dirExists(env) {
			return http.Dir(env)
		}
		log.Fatalf("static dir not found in WIFI_RADAR_STATIC_DIR: %s", env)
	}

	cwd := mustCwd()
	if dirExists(filepath.Join(cwd, "web", "static")) {
		return http.Dir(filepath.Join(cwd, "web", "static"))
	}

	exe, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exe)
		if dirExists(filepath.Join(exeDir, "web", "static")) {
			return http.Dir(filepath.Join(exeDir, "web", "static"))
		}
		if dirExists(filepath.Join(exeDir, "..", "web", "static")) {
			return http.Dir(filepath.Join(exeDir, "..", "web", "static"))
		}
	}

	staticFS, err := fs.Sub(webassets.Static, "static")
	if err != nil {
		log.Fatalf("embedded static assets unavailable: %v", err)
	}
	return http.FS(staticFS)
}

func staticHandler(staticFS http.FileSystem, raspberryUI bool) http.Handler {
	fileServer := http.FileServer(staticFS)
	if !raspberryUI {
		return fileServer
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			serveStaticFile(w, r, staticFS, "rb.html")
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func serveStaticFile(w http.ResponseWriter, r *http.Request, staticFS http.FileSystem, name string) {
	file, err := staticFS.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, name, info.ModTime(), file)
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

	fmt.Println(paint("Select network to track:", ansiBold, ansiYellow))
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
		fmt.Printf("  %s %s  %s  %s  %s\n",
			paint(fmt.Sprintf("%d)", i+1), ansiYellow, ansiBold),
			paint(ssid, ansiGreen, ansiBold),
			paint(n.BSSID, ansiDim),
			paint(signal, ansiCyan),
			paint(freq, ansiBlue),
		)
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print(paint("Enter number: ", ansiMagenta, ansiBold))
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
			fmt.Println(paint("Invalid number.", ansiRed, ansiBold))
			continue
		}
		if choice < 1 || choice > len(networks) {
			fmt.Println(paint("Out of range.", ansiRed, ansiBold))
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
	fmt.Println(paint("Select interface:", ansiBold, ansiYellow))
	for i, name := range ifs {
		fmt.Printf("  %s %s\n", paint(fmt.Sprintf("%d)", i+1), ansiYellow, ansiBold), paint(name, ansiGreen, ansiBold))
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print(paint("Enter number: ", ansiMagenta, ansiBold))
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
			fmt.Println(paint("Invalid number.", ansiRed, ansiBold))
			continue
		}
		if choice < 1 || choice > len(ifs) {
			fmt.Println(paint("Out of range.", ansiRed, ansiBold))
			continue
		}
		return ifs[choice-1], nil
	}
}

func openBrowserForDisplay(listen string, raspberryUI bool) {
	time.Sleep(300 * time.Millisecond)
	url := browserURL(listen)
	if raspberryUI {
		args := []string{"--kiosk", "--disable-infobars", "--noerrdialogs", url}
		for _, name := range []string{"chromium-browser", "chromium"} {
			if err := exec.Command(name, args...).Start(); err == nil {
				return
			}
		}
		for _, name := range []string{"firefox", "firefox-esr"} {
			if err := exec.Command(name, "--kiosk", url).Start(); err == nil {
				return
			}
		}
	}
	if err := exec.Command("firefox", url).Start(); err == nil {
		return
	}
	_ = exec.Command("xdg-open", url).Start()
}

func browserURL(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Sprintf("http://%s/", listen)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/"
}
