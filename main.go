package main

import (
	"bufio"
	"context"
	"embed"
	"fmt"
	"html/template"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/joho/godotenv"
)

//go:embed templates/index.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

var (
	tmpl               *template.Template
	nutServers         []NUTServer
	modelNames         map[string]string
	olChrgOnlineModels map[string]bool
	refreshSeconds     int

	cache   PageData
	cacheMu sync.RWMutex
)

type NUTServer struct {
	Host string
	Port int
}

type NUTClient struct {
	conn   net.Conn
	reader *bufio.Reader
}

type UPSCard struct {
	IsError      bool
	Host         string
	ErrorMessage string
	DisplayName  string
	Model        string
	StatusLabel  string
	StatusColor  string
	BorderClass  string
	Charge       int
	ChargeClass  string
	MOC          string
	NominalPower string
	Runtime      string
	BatteryVolt  string
	InputVolt    string
	OutputVolt   string
}

type PageData struct {
	UPSCards       []UPSCard
	LastUpdated    string
	RefreshSeconds int
}

func connectNUT(host string, port int) (*NUTClient, error) {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), 5*time.Second)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	return &NUTClient{conn: conn, reader: bufio.NewReader(conn)}, nil
}

func (c *NUTClient) sendCommand(cmd string) ([]string, error) {
	if _, err := fmt.Fprintf(c.conn, "%s\n", cmd); err != nil {
		return nil, err
	}
	var lines []string
	for {
		line, err := c.reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		lines = append(lines, line)
		if strings.HasPrefix(line, "END ") {
			break
		}
		if strings.HasPrefix(line, "ERR ") {
			return nil, fmt.Errorf("%s", line)
		}
	}
	return lines, nil
}

func (c *NUTClient) ListUPS() ([]string, error) {
	lines, err := c.sendCommand("LIST UPS")
	if err != nil {
		return nil, err
	}
	var result []string
	for _, line := range lines {
		if strings.HasPrefix(line, "UPS ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				result = append(result, parts[1])
			}
		}
	}
	return result, nil
}

func (c *NUTClient) ListVars(upsName string) (map[string]string, error) {
	lines, err := c.sendCommand("LIST VAR " + upsName)
	if err != nil {
		return nil, err
	}
	vars := make(map[string]string)
	prefix := "VAR " + upsName + " "
	for _, line := range lines {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		rest := line[len(prefix):]
		idx := strings.Index(rest, " ")
		if idx < 0 {
			continue
		}
		name := rest[:idx]
		value := rest[idx+1:]
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			value = value[1 : len(value)-1]
		}
		vars[name] = value
	}
	return vars, nil
}

func (c *NUTClient) Close() {
	fmt.Fprintf(c.conn, "LOGOUT\n")
	c.conn.Close()
}

func parseNUTServers() []NUTServer {
	var servers []NUTServer
	for _, entry := range strings.Split(os.Getenv("NUT_SERVERS"), ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, ":") {
			parts := strings.SplitN(entry, ":", 2)
			host := strings.TrimSpace(parts[0])
			port, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				port = 3493
			}
			servers = append(servers, NUTServer{Host: host, Port: port})
		} else {
			servers = append(servers, NUTServer{Host: entry, Port: 3493})
		}
	}
	return servers
}

func parseModelNames() map[string]string {
	names := make(map[string]string)
	for _, entry := range strings.Split(os.Getenv("NUT_MODEL_NAMES"), ",") {
		entry = strings.TrimSpace(entry)
		if strings.Contains(entry, ":") {
			parts := strings.SplitN(entry, ":", 2)
			names[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return names
}

func parseOLChrgOnlineModels() map[string]bool {
	models := make(map[string]bool)
	for _, m := range strings.Split(os.Getenv("NUT_OL_CHRG_AS_ONLINE"), ",") {
		m = strings.TrimSpace(m)
		if m != "" {
			models[m] = true
		}
	}
	return models
}

func safeFloat(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0.0
	}
	return f
}

func safeInt(s string) int {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return int(f)
}

func fmt1(s string) string {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return "N/A"
	}
	return strconv.FormatFloat(math.Round(f*10)/10, 'f', 1, 64)
}

func fmtRuntime(seconds float64) string {
	minutes := int(seconds / 60)
	if minutes < 60 {
		return fmt.Sprintf("%d min", minutes)
	}
	hours := minutes / 60
	mins := minutes % 60
	if mins > 0 {
		return fmt.Sprintf("%dh %dmin", hours, mins)
	}
	return fmt.Sprintf("%dh", hours)
}

func getChargeClass(charge int) string {
	switch {
	case charge == 100:
		return "bg-success"
	case charge < 26:
		return "bg-danger"
	case charge < 50:
		return "bg-warning"
	case charge < 75:
		return "bg-info"
	default:
		return "bg-primary"
	}
}

func getStatusInfo(status, model string, charge int) (string, string) {
	switch status {
	case "OL":
		return "Online", "bg-success"
	case "OB DISCHRG":
		return "Zasilanie bateryjne", "bg-danger"
	case "OL CHRG":
		if olChrgOnlineModels[model] && charge >= 100 {
			return "Online", "bg-success"
		}
		return "Ładowanie", "bg-warning"
	case "OL CHRG LB":
		return "Ładowanie (Bateria prawie pusta)", "bg-warning"
	default:
		return "Nieznane", "bg-info"
	}
}

func getBorderClass(statusColor string) string {
	return "border-status-" + strings.TrimPrefix(statusColor, "bg-")
}

func fetchServer(server NUTServer) []UPSCard {
	client, err := connectNUT(server.Host, server.Port)
	if err != nil {
		return []UPSCard{{IsError: true, Host: server.Host, ErrorMessage: err.Error()}}
	}
	defer client.Close()

	upsList, err := client.ListUPS()
	if err != nil {
		return []UPSCard{{IsError: true, Host: server.Host, ErrorMessage: err.Error()}}
	}

	var cards []UPSCard
	for _, upsName := range upsList {
		vars, err := client.ListVars(upsName)
		if err != nil {
			cards = append(cards, UPSCard{
				IsError:      true,
				Host:         server.Host,
				ErrorMessage: fmt.Sprintf("%s: %v", upsName, err),
			})
			continue
		}

		realpower := safeFloat(vars["ups.realpower.nominal"])
		load := safeFloat(vars["ups.load"])
		runtime := safeFloat(vars["battery.runtime"])
		charge := safeInt(vars["battery.charge"])
		model := vars["device.model"]
		status := vars["ups.status"]

		moc := math.Round(realpower*load/100*10) / 10
		statusLabel, statusColor := getStatusInfo(status, model, charge)

		displayName := model
		if name, ok := modelNames[model]; ok {
			displayName = name
		}

		cards = append(cards, UPSCard{
			IsError:      false,
			Host:         server.Host,
			DisplayName:  displayName,
			Model:        model,
			StatusLabel:  statusLabel,
			StatusColor:  statusColor,
			BorderClass:  getBorderClass(statusColor),
			Charge:       charge,
			ChargeClass:  getChargeClass(charge),
			MOC:          strconv.FormatFloat(moc, 'f', 1, 64),
			NominalPower: fmt1(vars["ups.realpower.nominal"]),
			Runtime:      fmtRuntime(runtime),
			BatteryVolt:  fmt1(vars["battery.voltage"]),
			InputVolt:    fmt1(vars["input.voltage"]),
			OutputVolt:   fmt1(vars["output.voltage"]),
		})
	}
	return cards
}

// fetchAllUPS queries all NUT servers in parallel, preserving server order.
func fetchAllUPS() PageData {
	results := make([][]UPSCard, len(nutServers))
	var wg sync.WaitGroup

	for i, server := range nutServers {
		wg.Add(1)
		go func(i int, srv NUTServer) {
			defer wg.Done()
			results[i] = fetchServer(srv)
		}(i, server)
	}
	wg.Wait()

	var cards []UPSCard
	for _, r := range results {
		cards = append(cards, r...)
	}

	return PageData{
		UPSCards:       cards,
		LastUpdated:    time.Now().Format("15:04:05"),
		RefreshSeconds: refreshSeconds,
	}
}

func startCacheRefresh(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			data := fetchAllUPS()
			cacheMu.Lock()
			cache = data
			cacheMu.Unlock()
		}
	}()
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	cacheMu.RLock()
	data := cache
	cacheMu.RUnlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("Template error: %v", err)
	}
}

func main() {
	_ = godotenv.Load()

	nutServers = parseNUTServers()
	modelNames = parseModelNames()
	olChrgOnlineModels = parseOLChrgOnlineModels()

	refreshSeconds = 30
	if v := os.Getenv("REFRESH_INTERVAL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			refreshSeconds = n
		}
	}

	var err error
	tmpl, err = template.ParseFS(templateFS, "templates/index.html")
	if err != nil {
		log.Fatal("Failed to parse template: ", err)
	}

	log.Printf("VanillaNUT: initial data fetch...")
	cache = fetchAllUPS()

	startCacheRefresh(time.Duration(refreshSeconds) * time.Second)

	http.Handle("/static/", http.FileServer(http.FS(staticFS)))
	http.HandleFunc("/", indexHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "5000"
	}

	srv := &http.Server{Addr: ":" + port}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		<-sigCh
		log.Println("VanillaNUT: shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("Shutdown error: %v", err)
		}
	}()

	log.Printf("VanillaNUT starting on :%s (refresh every %ds)", port, refreshSeconds)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
