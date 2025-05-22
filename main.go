package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"strings"

	tiny "lorde.tech/tinyurl/shortener"
)

// TODO: Add input validation for URLs an IDs

func main() {
	shortener := tiny.NewShortener()
	shortener.LoadFromLog()

	http.HandleFunc("GET /{id}", translateTinyUrl(shortener))
	http.HandleFunc("GET /api/urls", listTinyUrls(shortener))
	http.HandleFunc("GET /api/urls/{id}", fetchTinyUrl(shortener))
	http.HandleFunc("POST /api/urls", createTinyUrl(shortener))
	http.HandleFunc("DELETE /api/urls/{id}", deleteTinyUrl(shortener))
	fmt.Println("[SERVER] Listening on port 1337")
	http.ListenAndServe("localhost:1337", nil)
}

func translateTinyUrl(s *tiny.Shortener) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		prefix := generatePrefix(r, "TRANSLATE")
		id := r.PathValue("id")
		url, err := s.Translate(id)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			fmt.Printf("%s [ERROR] Failed to process id \"%s\" -> %s\n", prefix, id, err.Error())
			url = "[NOT FOUND]"
		} else {
			w.Header().Set("Location", url)
			w.Header().Set("Cache-Control", "max-age=86400")
			w.WriteHeader(http.StatusFound)
		}
		fmt.Printf("%s %s -> %s\n", prefix, id, url)
	}
}

func listTinyUrls(s *tiny.Shortener) http.HandlerFunc {
	const localhostIPV6 = "0000:0000:0000:0000:0000:0000:0000:0001"
	const localhostIPV4 = "0000:0000:0000:0000:0000:ffff:7f00:0001"
	return func(w http.ResponseWriter, r *http.Request) {
		prefix := generatePrefix(r, "API/LIST")
		if ip := getClientIp(r); ip != localhostIPV4 && ip != localhostIPV6 {
			fmt.Printf("%s [ERROR] Forbidden when not from localhost\n", prefix)
			forbiddenRequest(w)
			return
		}
		fmt.Printf("%s Starting...'\n", prefix)
		res := []TinyUrlMapping{}
		for k, v := range s.ListAll() {
			res = append(res, TinyUrlMapping{From: k, To: v})
		}
		setCotentTypeToJson(w)
		json.NewEncoder(w).Encode(res)
		fmt.Printf("%s Done!\n", prefix)
	}
}

func createTinyUrl(s *tiny.Shortener) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		prefix := generatePrefix(r, "API/ADD")
		var body Request = Request{}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&body)
		if err != nil {
			fmt.Printf("%s [ERROR] %s\n", prefix, err.Error())
			badRequest(w, err)
			return
		}
		fmt.Printf("%s body -> '%s'\n", prefix, body)
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
						fmt.Printf("%s [ERROR] Could not recover from failed insertion: id -> %s error -> %s\n", prefix, id, err.Error())
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
		fmt.Printf("%s %s -> %s\n", prefix, id, url)
	}
}

func deleteTinyUrl(s *tiny.Shortener) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		prefix := generatePrefix(r, "API/DELETE")
		fmt.Printf("%s ID -> '%s'\n", prefix, id)
		_, err := s.Translate(id)
		if err != nil {
			fmt.Printf("%s [ERROR] %s\n", prefix, err.Error())
			badRequest(w, err)
			return
		}
		err = s.Remove(id)
		if err != nil {
			fmt.Printf("%s [ERROR] %s\n", prefix, err.Error())
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Printf("%s Deleted ID '%s'\n", prefix, id)
	}
}

func fetchTinyUrl(s *tiny.Shortener) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		prefix := generatePrefix(r, "API/FETCH")
		target, err := s.Translate(id)
		if err != nil {
			fmt.Printf("%s [ERROR] %s\n", prefix, err.Error())
			w.WriteHeader(http.StatusNotFound)
			return
		}
		setCotentTypeToJson(w)
		json.NewEncoder(w).Encode(TinyUrlMapping{From: id, To: target})
		fmt.Printf("%s Fetched ID '%s'\n", prefix, id)
	}
}

func generatePrefix(r *http.Request, tag string) string {
	return fmt.Sprintf("[%s] [%s]", getClientIp(r), tag)
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
