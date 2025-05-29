package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"time"

	swgui "github.com/swaggest/swgui/v5cdn"

	rl "lorde.tech/tinyurl/rate_limiter"
	tiny "lorde.tech/tinyurl/shortener"
)

//go:embed static/openapi.yaml
var openapi string

func main() {
	shortener := tiny.NewShortener()
	shortener.LoadFromLog()

	log := log.New(
		os.Stdout,
		"[SERVER] ",
		log.LUTC|log.Ldate|log.Ltime|log.Lmsgprefix,
	)

	apiLimiter := rl.NewRateLimiter(10)
	translateLimiter := rl.NewRateLimiter(100)

	// Compaction routine
	go func() {
		for range time.Tick(24 * time.Hour) {
			go shortener.CompactLog()
			go apiLimiter.Compact()
			go translateLimiter.Compact()
		}
	}()

	apiWrapper := func(f http.HandlerFunc) http.HandlerFunc {
		return rateLimited(apiLimiter, f)
	}

	http.HandleFunc("GET /{id}", rateLimited(translateLimiter, translateTinyUrl(shortener)))
	http.HandleFunc("GET /api/urls", apiWrapper(listTinyUrls(shortener)))
	http.HandleFunc("POST /api/urls", apiWrapper(createTinyUrl(shortener)))
	http.HandleFunc("GET /api/urls/{id}", apiWrapper(fetchTinyUrl(shortener)))
	http.HandleFunc("DELETE /api/urls/{id}", apiWrapper(deleteTinyUrl(shortener)))

	http.HandleFunc("/", redicrectToDocs)
	http.Handle("/api/docs/", swgui.New("Tiny URL", "/api/docs/openapi.yaml", "/api/docs/"))
	http.HandleFunc("/api/docs/openapi.yaml", serveOpenapi)

	log.Println("Listening on port 1337")
	http.ListenAndServe("localhost:1337", nil)
}

func rateLimited(limiter *rl.RateLimiter, f http.HandlerFunc) http.HandlerFunc {
	red := func(s string) string {
		return fmt.Sprintf("\x1b[1;31m%s\x1b[0m", s)
	}
	erro_log_format := red("REQUEST BLOCKED") + " -> %s"
	return func(w http.ResponseWriter, r *http.Request) {
		log := newLogger(r, "RATE LIMIT")
		if !limiter.ShouldServe(getClientIp(r)) {
			log.Error(erro_log_format, r.URL.Path)
			http.Error(w, "Too many requests", http.StatusTooManyRequests)
			return
		}
		f(w, r)
	}
}

func redicrectToDocs(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/api/docs/", http.StatusFound)
}

func serveOpenapi(w http.ResponseWriter, r *http.Request) {
	log := newLogger(r, "API/DOCS")
	log.Info("openapi.yaml")
	fmt.Fprint(w, openapi)
}

func translateTinyUrl(s *tiny.Shortener) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log := newLogger(r, "TRANSLATE")
		id := r.PathValue("id")
		url, err := s.Translate(id)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			log.Error("Failed to process id \"%s\" -> %s\n", id, err.Error())
			url = "[NOT FOUND]"
		} else {
			w.Header().Set("Location", url)
			w.Header().Set("Cache-Control", "max-age=86400")
			w.WriteHeader(http.StatusFound)
		}
		log.Info("%s -> %s", id, url)
	}
}

func listTinyUrls(s *tiny.Shortener) http.HandlerFunc {
	const localhostIPV6 = "0000:0000:0000:0000:0000:0000:0000:0001"
	const localhostIPV4 = "0000:0000:0000:0000:0000:ffff:7f00:0001"
	return func(w http.ResponseWriter, r *http.Request) {
		log := newLogger(r, "API/LIST")
		if ip := getClientIp(r); ip != localhostIPV4 && ip != localhostIPV6 {
			log.Error("Forbidden when not from localhost")
			forbiddenRequest(w)
			return
		}
		log.Info("Starting...")
		res := []TinyUrlMapping{}
		for k, v := range s.ListAll() {
			res = append(res, TinyUrlMapping{From: k, To: v})
		}
		setCotentTypeToJson(w)
		json.NewEncoder(w).Encode(res)
		log.Info("Done!")
	}
}

func createTinyUrl(s *tiny.Shortener) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log := newLogger(r, "API/ADD")
		var body Request = Request{}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&body)
		if err != nil {
			log.DefaultError(err)
			badRequest(w, err)
			return
		}
		bodyString, _ := json.Marshal(body)
		log.Info("body -> '%s'\n", string(bodyString))
		url := body.Target
		id := body.Id
		if id == "" {
			id, err = s.Insert(url)
		} else {
			err = s.InsertCustom(id, url)
		}

		defer func(s *tiny.Shortener) {
			if r := recover(); r != nil {
				_, err := s.Translate(id)
				if err == nil {
					err = s.Remove(id)
					if err != nil {
						log.Error("Could not recover from failed insertion: id -> %s error -> %s\n", id, err.Error())
					}
				}
				w.WriteHeader(http.StatusInternalServerError)
			}
		}(s)

		if err != nil {
			badRequest(w, err)
			id = "[ERROR]"
		} else {
			setCotentTypeToJson(w)
			json.NewEncoder(w).Encode(NewTinyUrlResponse{TinyUrl: id})
		}
		log.Info("%s -> %s", id, url)
	}
}

func deleteTinyUrl(s *tiny.Shortener) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log := newLogger(r, "API/DELETE")
		id := r.PathValue("id")
		log.Info("ID -> '%s'", id)
		_, err := s.Translate(id)
		if err != nil {
			log.DefaultError(err)
			badRequest(w, err)
			return
		}
		err = s.Remove(id)
		if err != nil {
			log.DefaultError(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		log.Info("Deleted ID '%s'", id)
	}
}

func fetchTinyUrl(s *tiny.Shortener) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log := newLogger(r, "API/FETCH")
		id := r.PathValue("id")
		target, err := s.Translate(id)
		if err != nil {
			log.DefaultError(err)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		setCotentTypeToJson(w)
		json.NewEncoder(w).Encode(TinyUrlMapping{From: id, To: target})
		log.Info("Fetched ID '%s'", id)
	}
}

type Logger struct {
	l *log.Logger
}

func newLogger(r *http.Request, tag string) *Logger {
	return &Logger{
		l: log.New(
			os.Stdout,
			fmt.Sprintf("[%s|%s] ", getClientIp(r), tag),
			log.LUTC|log.Ldate|log.Ltime|log.Lmsgprefix,
		),
	}

}

func (l *Logger) Info(format string, s ...any) {
	l.l.Printf(format+"\n", s...)
}

func (l *Logger) Error(format string, s ...any) {
	l.l.Printf("[ERROR] "+format+"\n", s...)
}

func (l *Logger) DefaultError(err error) {
	l.Error(err.Error())
}

func getClientIp(r *http.Request) string {
	ip := getForwardedHost(r)
	if ip == "" {
		ip = getRemoteAddr(r)
	}
	return ip
}

func getForwardedHost(r *http.Request) string {
	hosts := r.Header.Get("X-Forwarded-For")
	if hosts == "" {
		return ""
	}

	ip := strings.Split(hosts, ",")[0]
	ip = strings.Trim(ip, " ")

	return normalizeIp(parseIp(ip))
}

func setCotentTypeToJson(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
}

func forbiddenRequest(w http.ResponseWriter) {
	w.WriteHeader(http.StatusForbidden)
}

func badRequest(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusBadRequest)
	setCotentTypeToJson(w)
	json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
}

func getRemoteAddr(r *http.Request) string {
	return normalizeIp(netip.MustParseAddrPort(r.RemoteAddr).Addr())
}

func parseIp(ip string) netip.Addr {
	addr, err := netip.ParseAddrPort(ip)
	if err != nil {
		return netip.MustParseAddr(ip)
	}
	return addr.Addr()
}

func normalizeIp(ip netip.Addr) string {
	return netip.AddrFrom16(ip.As16()).StringExpanded()
}

type Request struct {
	Id     string `json:"from,omitempty"`
	Target string `json:"to"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type NewTinyUrlResponse struct {
	TinyUrl string `json:"tiny_url"`
}

type TinyUrlMapping struct {
	From string `json:"from"`
	To   string `json:"to"`
}
