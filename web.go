package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
)

type webRequest struct {
	Args []json.RawMessage `json:"args"`
}

type webResponse struct {
	Result any    `json:"result"`
	Error  string `json:"error,omitempty"`
}

type webSession struct {
	Token     string `json:"token"`
	Workspace bool   `json:"workspace"`
}

func runWebMode(app *App, host string, port, remoteVaultPort int, openBrowser, allowRemote bool) error {
	if port < 1024 || port > 65535 {
		return fmt.Errorf("port must be between 1024 and 65535")
	}
	if remoteVaultPort != 0 && (remoteVaultPort < 1024 || remoteVaultPort > 65535 || remoteVaultPort == port) {
		return fmt.Errorf("remote Vault port must be between 1024 and 65535 and differ from the local port")
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return fmt.Errorf("host is required")
	}
	app.webMode = true
	app.startup(webStartupContext())
	defer app.shutdown(webStartupContext())

	token, err := randomWebToken()
	if err != nil {
		return err
	}
	webFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		return fmt.Errorf("load web assets: %w", err)
	}
	address := net.JoinHostPort(host, strconv.Itoa(port))
	servers := []*http.Server{{Addr: address, Handler: webRouter(app, token, webFS, allowRemote, true)}}
	browserHost := host
	if host == "0.0.0.0" || host == "::" {
		browserHost = "127.0.0.1"
	}
	url := "http://" + net.JoinHostPort(browserHost, strconv.Itoa(port)) + "/"
	log.Printf("DevHub Web is running locally at %s", url)
	if openBrowser {
		if err := openDefaultBrowser(url); err != nil {
			log.Printf("Could not open the browser automatically: %v", err)
		}
	}
	if remoteVaultPort != 0 {
		remoteToken, err := randomWebToken()
		if err != nil {
			return err
		}
		remoteAddress := net.JoinHostPort(host, strconv.Itoa(remoteVaultPort))
		servers = append(servers, &http.Server{
			Addr:    remoteAddress,
			Handler: webRouter(app, remoteToken, webFS, allowRemote, false),
		})
		log.Printf("DevHub Vault-only access is listening on %s", remoteAddress)
	}

	errorsByServer := make(chan error, len(servers))
	for _, server := range servers {
		go func(current *http.Server) { errorsByServer <- current.ListenAndServe() }(server)
	}
	err = <-errorsByServer
	for _, server := range servers {
		_ = server.Close()
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func webRouter(app *App, token string, webFS fs.FS, allowRemote, workspaceEnabled bool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/session", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !allowRemote && !isLoopbackRequest(r) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(webSession{Token: token, Workspace: workspaceEnabled})
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		if (!allowRemote && !isLoopbackRequest(r)) || !validWebToken(r.Header.Get("X-DevHub-Token"), token) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		method := strings.TrimPrefix(r.URL.Path, "/api/")
		if !workspaceEnabled && isWorkspaceWebMethod(method) {
			writeWebJSON(w, http.StatusForbidden, webResponse{Error: "Workspace is available only on the host computer"})
			return
		}
		var request webRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&request); err != nil {
			writeWebJSON(w, http.StatusBadRequest, webResponse{Error: "Invalid request"})
			return
		}
		result, err := callWebAPI(app, method, request.Args)
		if err != nil {
			writeWebJSON(w, http.StatusBadRequest, webResponse{Error: err.Error()})
			return
		}
		writeWebJSON(w, http.StatusOK, webResponse{Result: result})
	})

	files := http.FileServer(http.FS(webFS))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'")
		if r.URL.Path != "/" {
			if _, err := fs.Stat(webFS, strings.TrimPrefix(r.URL.Path, "/")); err == nil {
				files.ServeHTTP(w, r)
				return
			}
		}
		index, err := fs.ReadFile(webFS, "index.html")
		if err != nil {
			http.Error(w, "Web assets are unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
	return mux
}

func isWorkspaceWebMethod(method string) bool {
	switch method {
	case "ListProjects", "ChooseProjectDirectory", "AddProject", "UpdateProject", "DeleteProject", "OpenProject", "OpenProjectWith":
		return true
	default:
		return false
	}
}

func callWebAPI(app *App, method string, args []json.RawMessage) (any, error) {
	var (
		text      string
		textTwo   string
		textThree string
		textFour  string
		textFive  string
		textSix   string
		textSeven string
		id        int64
		items     []string
	)
	switch method {
	case "IsInitialized":
		return app.IsInitialized(), nil
	case "IsUnlocked":
		return app.IsUnlocked(), nil
	case "Lock":
		app.Lock()
		return nil, nil
	case "GetCategories":
		return app.GetCategories()
	case "ListProjects":
		return app.ListProjects()
	case "ChooseProjectDirectory":
		return app.ChooseProjectDirectory()
	case "Initialize":
		return nil, decodeCall(args, &text, func() error { return app.Initialize(text) })
	case "Unlock":
		return nil, decodeCall(args, &text, func() error { return app.Unlock(text) })
	case "AddCategory":
		return callOneString(args, app.AddCategory)
	case "DeleteCategory":
		return nil, decodeCall(args, &id, func() error { return app.DeleteCategory(id) })
	case "RenameCategory":
		return nil, decodeCall(args, &id, &text, func() error { return app.RenameCategory(id, text) })
	case "ListEntries":
		return callOneInt64(args, app.ListEntries)
	case "SearchEntries":
		return callOneString(args, app.SearchEntries)
	case "GetEntryHistory":
		return callOneString(args, app.GetEntryHistory)
	case "GetTOTPCode":
		return callOneString(args, app.GetTOTPCode)
	case "GeneratePassword":
		return callOneInt(args, app.GeneratePassword)
	case "DeleteEntries":
		return nil, decodeCall(args, &items, &text, func() error { return app.DeleteEntries(items, text) })
	case "AddEntry":
		return nil, decodeCall(args, &id, &text, &textTwo, &textThree, &textFour, &textFive, &textSix, func() error { return app.AddEntry(id, text, textTwo, textThree, textFour, textFive, textSix) })
	case "UpdateEntry":
		return nil, decodeCall(args, &text, &id, &textTwo, &textThree, &textFour, &textFive, &textSix, &textSeven, func() error {
			return app.UpdateEntry(text, id, textTwo, textThree, textFour, textFive, textSix, textSeven)
		})
	case "ExportVault":
		return callOneString(args, app.ExportVault)
	case "ImportVault":
		return callOneString(args, app.ImportVault)
	case "AddProject":
		return callFourStrings(args, app.AddProject)
	case "UpdateProject":
		return nil, decodeCall(args, &id, &text, &textTwo, &textThree, &textFour, func() error { return app.UpdateProject(id, text, textTwo, textThree, textFour) })
	case "DeleteProject":
		return nil, decodeCall(args, &id, func() error { return app.DeleteProject(id) })
	case "OpenProject":
		return nil, decodeCall(args, &id, func() error { return app.OpenProject(id) })
	case "OpenProjectWith":
		return nil, decodeCall(args, &id, &text, func() error { return app.OpenProjectWith(id, text) })
	default:
		return nil, fmt.Errorf("unknown API method")
	}
}

func decodeCall(args []json.RawMessage, targets ...any) error {
	callback, ok := targets[len(targets)-1].(func() error)
	if !ok {
		return errors.New("invalid API handler")
	}
	values := targets[:len(targets)-1]
	if len(args) != len(values) {
		return fmt.Errorf("invalid argument count")
	}
	for index, target := range values {
		if err := json.Unmarshal(args[index], target); err != nil {
			return fmt.Errorf("invalid argument")
		}
	}
	return callback()
}

func callOneString[T any](args []json.RawMessage, function func(string) (T, error)) (T, error) {
	var value string
	if err := decodeCall(args, &value, func() error { return nil }); err != nil {
		var empty T
		return empty, err
	}
	return function(value)
}
func callOneInt[T any](args []json.RawMessage, function func(int) (T, error)) (T, error) {
	var value int
	if err := decodeCall(args, &value, func() error { return nil }); err != nil {
		var empty T
		return empty, err
	}
	return function(value)
}
func callOneInt64[T any](args []json.RawMessage, function func(int64) (T, error)) (T, error) {
	var value int64
	if err := decodeCall(args, &value, func() error { return nil }); err != nil {
		var empty T
		return empty, err
	}
	return function(value)
}
func callFourStrings[T any](args []json.RawMessage, function func(string, string, string, string) (T, error)) (T, error) {
	var one, two, three, four string
	if err := decodeCall(args, &one, &two, &three, &four, func() error { return nil }); err != nil {
		var empty T
		return empty, err
	}
	return function(one, two, three, four)
}

func writeWebJSON(w http.ResponseWriter, status int, response webResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}
func randomWebToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
func isLoopbackRequest(request *http.Request) bool {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	return err == nil && net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}
func validWebToken(received, expected string) bool {
	if received == "" || expected == "" || len(received) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(received), []byte(expected)) == 1
}
func openDefaultBrowser(url string) error {
	return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url).Start()
}
