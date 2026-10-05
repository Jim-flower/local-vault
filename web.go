package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type webRequest struct {
	Args []json.RawMessage `json:"args"`
}

type webResponse struct {
	Result any    `json:"result"`
	Error  string `json:"error,omitempty"`
}

const maxVaultUploadSize = 16 << 20

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

	webFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		return fmt.Errorf("load web assets: %w", err)
	}
	address := net.JoinHostPort(host, strconv.Itoa(port))
	servers := []*http.Server{newWebServer(address, webRouter(app, "", webFS, allowRemote, true))}
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
		remoteAddress := net.JoinHostPort(host, strconv.Itoa(remoteVaultPort))
		servers = append(servers, newWebServer(remoteAddress, webRouter(app, "", webFS, allowRemote, false)))
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

func newWebServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
}

func webRouter(app *App, _ string, webFS fs.FS, allowRemote, workspaceEnabled bool) http.Handler {
	sessions := app.browserSessionStore()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth", func(w http.ResponseWriter, r *http.Request) {
		handleWebAuth(app, sessions, allowRemote, workspaceEnabled, w, r)
	})
	mux.HandleFunc("/api/auth/", func(w http.ResponseWriter, r *http.Request) {
		handleWebAuth(app, sessions, allowRemote, workspaceEnabled, w, r)
	})
	mux.HandleFunc("/api/export", func(w http.ResponseWriter, r *http.Request) {
		if !remoteWebAccessAllowed(r, allowRemote, workspaceEnabled) {
			writeWebJSON(w, http.StatusForbidden, webResponse{Error: "remote access requires HTTPS"})
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if _, _, ok := requireUnlockedVault(w, r, app, sessions); !ok {
			return
		}
		var request struct {
			Password string `json:"password"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&request); err != nil {
			writeWebJSON(w, http.StatusBadRequest, webResponse{Error: "Invalid export request"})
			return
		}
		if app.store == nil {
			writeWebJSON(w, http.StatusServiceUnavailable, webResponse{Error: "store not available"})
			return
		}
		tempDirectory, err := os.MkdirTemp("", "devhub-export-*")
		if err != nil {
			writeWebJSON(w, http.StatusInternalServerError, webResponse{Error: err.Error()})
			return
		}
		defer os.RemoveAll(tempDirectory)
		filename := "vault-export-" + time.Now().Format("20060102") + ".zip"
		path := filepath.Join(tempDirectory, filename)
		count, err := app.store.ExportToZIP(path, request.Password)
		if err != nil {
			writeWebJSON(w, http.StatusBadRequest, webResponse{Error: err.Error()})
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			writeWebJSON(w, http.StatusInternalServerError, webResponse{Error: err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-DevHub-Entry-Count", strconv.Itoa(count))
		_, _ = w.Write(data)
	})
	mux.HandleFunc("/api/import", func(w http.ResponseWriter, r *http.Request) {
		if !remoteWebAccessAllowed(r, allowRemote, workspaceEnabled) {
			writeWebJSON(w, http.StatusForbidden, webResponse{Error: "remote access requires HTTPS"})
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if _, _, ok := requireUnlockedVault(w, r, app, sessions); !ok {
			return
		}
		if app.store == nil {
			writeWebJSON(w, http.StatusServiceUnavailable, webResponse{Error: "store not available"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxVaultUploadSize)
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			writeWebJSON(w, http.StatusBadRequest, webResponse{Error: "The import ZIP is invalid or too large"})
			return
		}
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			writeWebJSON(w, http.StatusBadRequest, webResponse{Error: "Choose a Vault export ZIP file"})
			return
		}
		defer file.Close()
		temp, err := os.CreateTemp("", ".vault-import-*.zip")
		if err != nil {
			writeWebJSON(w, http.StatusInternalServerError, webResponse{Error: err.Error()})
			return
		}
		tempPath := temp.Name()
		defer os.Remove(tempPath)
		written, copyErr := io.Copy(temp, io.LimitReader(file, maxVaultUploadSize+1))
		closeErr := temp.Close()
		if copyErr != nil || closeErr != nil || written > maxVaultUploadSize {
			writeWebJSON(w, http.StatusBadRequest, webResponse{Error: "The import ZIP is invalid or too large"})
			return
		}
		result, err := app.store.ImportFromZIP(tempPath, r.FormValue("password"))
		if err != nil {
			writeWebJSON(w, http.StatusBadRequest, webResponse{Error: err.Error()})
			return
		}
		result.Path = filepath.Base(header.Filename)
		writeWebJSON(w, http.StatusOK, webResponse{Result: result})
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		if !remoteWebAccessAllowed(r, allowRemote, workspaceEnabled) {
			writeWebJSON(w, http.StatusForbidden, webResponse{Error: "remote access requires HTTPS"})
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		method := strings.TrimPrefix(r.URL.Path, "/api/")
		session, token, ok := requireBrowserSession(w, r, sessions)
		if !ok {
			return
		}
		if !workspaceEnabled && isWorkspaceWebMethod(method) {
			writeWebJSON(w, http.StatusForbidden, webResponse{Error: "Workspace is available only on the host computer"})
			return
		}
		if method == "IsUnlocked" {
			writeWebJSON(w, http.StatusOK, webResponse{Result: session.VaultUnlocked && app.IsUnlocked()})
			return
		}
		if method == "Lock" {
			app.Lock()
			sessions.lockAll()
			writeWebJSON(w, http.StatusOK, webResponse{})
			return
		}
		if method == "Unlock" {
			var unlock webRequest
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&unlock); err != nil || len(unlock.Args) != 1 {
				writeWebJSON(w, http.StatusBadRequest, webResponse{Error: "invalid unlock request"})
				return
			}
			var masterPassword string
			if err := json.Unmarshal(unlock.Args[0], &masterPassword); err != nil {
				writeWebJSON(w, http.StatusBadRequest, webResponse{Error: "invalid unlock request"})
				return
			}
			keys := authAttemptKeys(r, session.User.Username+":unlock")
			if rejectBlockedAuth(w, sessions, keys...) {
				return
			}
			if app.Unlock(masterPassword) != nil {
				sessions.recordAuthFailure(keys...)
				writeWebJSON(w, http.StatusUnauthorized, webResponse{Error: "incorrect master password"})
				return
			}
			sessions.clearAuthFailures(keys...)
			sessions.setVaultUnlocked(token, true)
			writeWebJSON(w, http.StatusOK, webResponse{})
			return
		}
		if method == "Initialize" {
			writeWebJSON(w, http.StatusForbidden, webResponse{Error: "complete the administrator setup first"})
			return
		}
		if method != "IsInitialized" && !isWorkspaceWebMethod(method) {
			if !session.VaultUnlocked || !app.IsUnlocked() {
				writeWebJSON(w, http.StatusLocked, webResponse{Error: "unlock the vault to continue"})
				return
			}
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
		if !remoteWebAccessAllowed(r, allowRemote, workspaceEnabled) {
			writeWebJSON(w, http.StatusForbidden, webResponse{Error: "remote access requires HTTPS"})
			return
		}
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
	return securityHeaders(mux)
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
func isLoopbackRequest(request *http.Request) bool {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	return err == nil && net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}
func openDefaultBrowser(url string) error {
	return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url).Start()
}
