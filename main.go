package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/netip"
	"os"
	"strings"

	tiny "lorde.tech/tinyurl/shortener"
)

// TODO: Add input validation for URLs an IDs

func main() {
	shortener := tiny.NewShortener()
	shortener.LoadFromLog()
	log := log.New(
		os.Stdout,
		"[SERVER] ",
		log.LUTC|log.Ldate|log.Ltime|log.Lmsgprefix,
	)

	http.HandleFunc("GET /{id}", translateTinyUrl(shortener))
	http.HandleFunc("GET /api/urls", listTinyUrls(shortener))
	http.HandleFunc("GET /api/urls/{id}", fetchTinyUrl(shortener))
	http.HandleFunc("POST /api/urls", createTinyUrl(shortener))
	http.HandleFunc("DELETE /api/urls/{id}", deleteTinyUrl(shortener))
	log.Println("Listening on port 1337")
	http.ListenAndServe("localhost:1337", nil)
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
	Id     string `json:"id,omitempty"`
	Target string `json:"target"`
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
