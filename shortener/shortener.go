package shortener

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"iter"
	"math"
	"math/rand/v2"
	"os"
	"strings"
	"sync"
)

const TINY_URL_LENGTH = 8
const ActionInsert = "+"
const ActionRemove = "-"

var TINY_URL_CHARTER_SET = [...]string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l",
	"m", "n", "o", "p", "q", "r", "s", "t", "u", "v", "w", "x", "y", "z", "A", "B", "C", "D", "E", "F",
	"G", "H", "I", "J", "K", "L", "M", "N", "O", "P", "Q", "R", "S", "T", "U", "V", "W", "X", "Y", "Z",
	"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "-", "_"}
var TINY_URL_CHARTER_SET_SIZE = int64(len(TINY_URL_CHARTER_SET))
var TINE_URL_ID_QUANITY = int64(math.Pow(float64(TINY_URL_CHARTER_SET_SIZE), float64(TINY_URL_LENGTH)))

type Shortener struct {
	urlMap *SkipList
	log    *os.File
	lock   sync.Mutex
}

func NewShortener() *Shortener {
	log, err := os.OpenFile("log", os.O_RDWR|os.O_CREATE, 0o0666)
	assertNoError(err)
	_, err = log.Seek(0, io.SeekEnd)
	assertNoError(err)
	return &Shortener{urlMap: NewSkiplist(), log: log}
}

func (s *Shortener) LoadFromLog() {
	s.lock.Lock()
	defer s.lock.Unlock()

	fmt.Printf("[SHORTENER/RESTORE FROM LOG] Starting...\n")

	_, err := s.log.Seek(0, io.SeekStart)
	assertNoError(err)
	scanner := bufio.NewScanner(s.log)
	fmt.Printf("[SHORTENER/RESTORE FROM LOG] Log ready, reading entries...\n")
	for scanner.Scan() {
		entry := scanner.Text()
		action, id, target := parseLogEntry(entry)
		switch action {
		case ActionInsert:
			fmt.Printf("[SHORTENER/RESTORE FROM LOG] [ADD] %s -> %s\n", id, target)
			assertNoError(s.restore(id, target))
		case ActionRemove:
			fmt.Printf("[SHORTENER/RESTORE FROM LOG] [REMOVE] %s -> %s\n", id, target)
			assertNoError(s.forceRemove(id))
		default:
			fmt.Printf("[SHORTENER/RESTORE FROM LOG] Skipping invalid log entry '%s'\n", entry)
		}
	}

	fmt.Printf("[SHORTENER/RESTORE FROM LOG] Done!\n")
}

func (s *Shortener) Insert(url string) (string, error) {
	for remainingAttempts := 128; remainingAttempts > 0; remainingAttempts-- {
		id := generateRandomId()
		err := s.urlMap.Insert(id, url)
		if err == nil {
			s.saveToLog(ActionInsert, id, url)
			return id, nil
		}
	}
	return "", errors.New("Could not generate an ID")
}

func (s *Shortener) InsertCustom(id string, url string) error {
	err := s.urlMap.Insert(id, url)
	if err == nil {
		s.saveToLog(ActionInsert, id, url)
	}
	return err
}

func (s *Shortener) Translate(id string) (string, error) {
	found, url := s.urlMap.Search(id)
	if !found {
		return "", errors.New("URL not found!")
	}
	return url, nil
}

func (s *Shortener) restore(id string, url string) error {
	return s.urlMap.Insert(id, url)
}

func (s *Shortener) ListAll() iter.Seq2[string, string] {
	return s.urlMap.Iter()
}

func (s *Shortener) Remove(id string) error {
	err := s.urlMap.Remove(id)
	if err != nil {
		return err
	}
	s.saveToLog(ActionRemove, id, "[DELETED]")
	return nil
}

func (s *Shortener) forceRemove(id string) error {
	return s.urlMap.Remove(id)
}

func parseLogEntry(entry string) (action string, id string, target string) {
	parts := strings.Split(entry, "|")
	action = parts[0]
	id = parts[1]
	target = parts[2]
	return
}

func (s *Shortener) saveToLog(action, id, target string) {
	s.lock.Lock()
	defer s.lock.Unlock()

	fmt.Fprintf(s.log, "%s|%s|%s\n", action, id, target)
	assertNoError(s.log.Sync())
}

func assertNoError(e error) {
	if e != nil {
		panic(e)
	}
}

func generateRandomId() string {
	return convertNumberToID(rand.Int64N(TINE_URL_ID_QUANITY))
}

func convertNumberToID(n int64) string {
	id := ""
	for n >= TINY_URL_CHARTER_SET_SIZE {
		id = TINY_URL_CHARTER_SET[n%TINY_URL_CHARTER_SET_SIZE] + id
		n = n / TINY_URL_CHARTER_SET_SIZE
	}
	id = TINY_URL_CHARTER_SET[n] + id
	for len(id) < TINY_URL_LENGTH {
		id = "0" + id
	}
	return id
}
