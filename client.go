package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/chrisfarms/yenc"

	NNTP "nzbfetch/nntp"
	NZB "nzbfetch/nzb"
	Types "nzbfetch/types"
)

func loadConfig() (conf Types.Config, err error) {
	b, err := os.ReadFile("client.conf")
	if err != nil {
		return conf, err
	}
	_, err = toml.Decode(string(b), &conf)
	return
}

// Reader fetches segments using its dedicated NNTP connection
func reader(id int, jobs <-chan Types.Segment, conn *Types.Connection, results chan<- Types.Segment, wg *sync.WaitGroup) {
	defer wg.Done()
	fmt.Printf("Reader %d started\n", id)
	for j := range jobs {
		j.Connection = conn
		segment, err := NNTP.FetchSegment(j)
		if err != nil {
			fmt.Printf("Reader %d error: %v\n", id, err)
			continue
		}
		// fmt.Println("Fetched segment:", segment.Article.Id)
		segment.OutName = j.OutName
		conn.LastGroup = segment.GroupUsed
		results <- segment
	}
	fmt.Printf("Reader %d finished\n", id)
}

func write(segment Types.Segment, outName string) {
	if outName == "" {
		outName = segment.Article.Id
	}
	part, err := yenc.Decode(bytes.NewReader(segment.Data))
	if err != nil {
		log.Printf("decode failed for %s: %v", segment.Article.Id, err)
		return
	}

	safeName := sanitizeFilename(outName)
	f, err := os.OpenFile("processed/"+safeName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("open %s: %v", safeName, err)
		return
	}
	defer f.Close()

	if _, err := f.Write(part.Body); err != nil {
		fmt.Printf("write %s: %v", safeName, err)
	} else {
		fmt.Printf("Wrote segment %d to %s\n", segment.Article.Number, safeName)
	}
}

func sanitizeFilename(filename string) string {
	invalidChars := []string{"\\", "/", ":", "*", "?", "\"", "<", ">", "|"}
	for _, c := range invalidChars {
		filename = strings.ReplaceAll(filename, c, "_")
	}
	return strings.TrimSpace(filename)
}

func filenameFromSubject(subject string) string {
	s := strings.ReplaceAll(subject, "&quot;", "\"")
	start := strings.IndexByte(s, '"')
	if start == -1 {
		return s
	}
	rest := s[start+1:]
	end := strings.IndexByte(rest, '"')
	if end == -1 {
		return s
	}
	return rest[:end]
}

func startDispatcher(results <-chan Types.Segment) {
	type queue struct {
		ch chan Types.Segment
	}
	perFile := sync.Map{}

	go func() {
		for seg := range results {
			key := seg.OutName
			if key == "" {
				key = seg.Article.Id
			}

			var q *queue
			v, ok := perFile.Load(key)
			if ok {
				q = v.(*queue)
			} else {
				q = &queue{ch: make(chan Types.Segment, 2000)}
				perFile.Store(key, q)
				go fileWriter(key, q.ch)
			}

			select {
			case q.ch <- seg:
			default:
				go func(s Types.Segment) { q.ch <- s }(seg)
			}
		}

		perFile.Range(func(_, v any) bool {
			close(v.(*queue).ch)
			return true
		})
	}()
}

func fileWriter(outName string, in <-chan Types.Segment) {
	expected := 1
	pending := make(map[int]Types.Segment)
	for s := range in {
		n := s.Article.Number
		pending[n] = s
		for {
			next, ok := pending[expected]
			if !ok {
				break
			}
			write(next, outName)
			delete(pending, expected)
			expected++
		}
	}
}
func main() {
	log.SetFlags(log.Lshortfile)
	config, err := loadConfig()
	if err != nil {
		log.Fatalf("Error parsing config: %v", err)
	}
	maxConnections := config.Connections
	fmt.Printf("Max Connections: %d\n", maxConnections)

	jobs := make(chan Types.Segment, 500)
	results := make(chan Types.Segment, 500)
	startDispatcher(results)

	// ---- Load NZB ----
	fmt.Println("Loading nzb file...")
	data, err := os.ReadFile("test.nzb")
	if err != nil {
		log.Fatalf("Failed to read NZB: %v", err)
	}
	nzb, err := NZB.NewString(string(data))
	if err != nil {
		log.Fatalf("Failed to parse NZB: %v", err)
	}
	fmt.Println("Successfully opened test.nzb")

	totalSize := NZB.GetTotalSize(nzb)
	fmt.Printf("Total NZB size: %d bytes\n", totalSize)

	// ---- Enqueue all jobs before starting readers ----
	go func() {
		for i := 0; i < len(nzb.Files); i++ {
			outName := filenameFromSubject(nzb.Files[i].Subject)
			for j := 0; j < len(nzb.Files[i].Segments); j++ {
				jobs <- Types.Segment{
					Article: nzb.Files[i].Segments[j],
					Groups:  nzb.Files[i].Groups,
					OutName: outName,
				}
			}
		}
		fmt.Println("All jobs added.")
		close(jobs)
	}()

	// ---- Start readers AFTER enqueue goroutine ----
	var wg sync.WaitGroup
	for i := 1; i <= maxConnections; i++ {
		conn, err := NNTP.Connect(config)
		if err != nil {
			log.Fatalf("Connection %d failed: %v", i, err)
		}
		fmt.Printf("Connection %d established\n", i)
		wg.Add(1)
		go reader(i, jobs, conn, results, &wg)
	}

	wg.Wait() // wait for all readers to finish
	close(results)
	fmt.Println("All readers finished.")
}
