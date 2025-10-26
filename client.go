package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml" // config as Tom's Obvious, Minimal Language
	"github.com/chrisfarms/yenc"

	//	"gopkg.in/yenc.v0"
	// decode yenc
	NNTP "nzbfetch/nntp"
	NZB "nzbfetch/nzb"
	Types "nzbfetch/types"
)

func loadConfig() (conf Types.Config, err error) {
	b, err := os.ReadFile("client.conf") // just pass the file name
	if err != nil {
		fmt.Print(err)
		return
	}
	str := string(b) // convert content to a 'string'
	_, err = toml.Decode(str, &conf)
	if err != nil {
		// handle error
		return
	}
	return
}

/*
workers take a connection c and a job j from respective pools,
fetch segment, and send segment to results channel where it's read by the download function
*/
func worker(id int, jobs <-chan Types.Segment, con <-chan *tls.Conn, results chan<- Types.Segment) {
	for c := range con {
		for j := range jobs {
			j.Connection = c
			segment, err := NNTP.FetchSegment(j)
			// fmt.Println("Worker", id, "fetched segment number:", segment.Article.Number)
			if err != nil {
				fmt.Print(err)
			}
			results <- segment
		}
	}
}

/*
write yenc file to disk, decode, append binary data
*/
func write(segment Types.Segment, outName string) {

	// Decode directly from memory to avoid temp file and contention
	part, err := yenc.Decode(bytes.NewReader(segment.Data))
	if err != nil {
		panic("decoding: " + err.Error())
	}
	// fmt.Println("Decoded: Filename", part.Name)

	// open file to append binary data
	safeName := sanitizeFilename(outName)
	f, err := os.OpenFile("processed/"+safeName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Print(err)
		return
	}
	defer f.Close()

	// write binary data to file
	_, err = f.Write(part.Body)
	if err != nil {
		fmt.Print(err)
		return
	}

	// fmt.Println("Successfully wrote segment to " + safeName)

}

func sanitizeFilename(filename string) string {
	// Define a list of invalid characters
	invalidChars := []string{"\\", "/", ":", "*", "?", "\"", "<", ">", "|"}

	// Replace invalid characters with underscores
	for _, char := range invalidChars {
		filename = strings.ReplaceAll(filename, char, "_")
	}
	// Remove whitespace from the filename
	filename = strings.TrimSpace(filename)

	return filename
}

// filenameFromSubject extracts the quoted filename from an NZB subject.
// Falls back to the whole subject if no quotes are found.
func filenameFromSubject(subject string) string {
	// Handle HTML entity for quotes just in case
	s := strings.ReplaceAll(subject, "&quot;", "\"")
	start := strings.IndexByte(s, '"')
	if start == -1 {
		return strings.TrimSpace(s)
	}
	rest := s[start+1:]
	end := strings.IndexByte(rest, '"')
	if end == -1 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:end])
}

/*
manage the download of files and segments contained in a single nzb file
*/
func download(nzb *NZB.Nzb, fileBegin int, segmentBegin int, connections chan *tls.Conn, maxWorkers int) {
	jobs := make(chan Types.Segment, 200)
	results := make(chan Types.Segment, 100)

	for w := 1; w <= maxWorkers; w++ { // 3 connections
		go worker(w, jobs, connections, results)
	}
	totalSize := NZB.GetTotalSize(nzb)
	bytesRemaining := totalSize
	fmt.Printf("Total NZB size: %d bytes\n", bytesRemaining)
	timeLast := time.Now()
	bytesIn := 0
	// for each file in nzb
	for i := fileBegin; i < len(nzb.Files); i++ {
		// create map to keep track of out-of-order segments
		segmentMap := make(map[int]Types.Segment)
		var expected = 1
		// for each segment
		fmt.Println("Working on new File: " + nzb.Files[i].Subject)
		// Derive the target output filename from the NZB subject
		targetName := filenameFromSubject(nzb.Files[i].Subject)

		// add each segment to jobs pool in a separate goroutine to avoid blocking
		enqueueDone := make(chan struct{})
		go func(fileIdx int) {
			defer close(enqueueDone)
			for j := segmentBegin; j < len(nzb.Files[fileIdx].Segments); j++ {
				jobs <- Types.Segment{Article: nzb.Files[fileIdx].Segments[j], Groups: nzb.Files[fileIdx].Groups}
			}
		}(i)
		for {
			segment := <-results
			// process new segment
			if segment.Article.Number == expected {
				write(segment, targetName)
				expected++
				// write segments stored in memory:
				for expected < len(nzb.Files[i].Segments)+1 {
					j := segmentMap[expected]
					// if next segment not found in memory
					if j.Article.Number == 0 {
						break
					}

					timeNow := time.Now()
					if timeLast.IsZero() || time.Since(timeLast) >= 5*time.Second {
						timeLast = timeNow
						bytesInThisInterval := bytesIn
						bytesIn = 0
						speed := float64(bytesInThisInterval) / 1024.0 // KB/s
						fmt.Printf("Download speed: %.2f KB/s\n", speed)
					}
					// bytesIn += int(j.Article.Bytes)
					bytesIn += len(segment.Data)

					// write segment
					write(j, targetName)
					delete(segmentMap, expected)
					expected++
					// update progress after advancing expected

				}
				// check if this is last segment
				if expected > len(nzb.Files[i].Segments) {
					// ensure the enqueuer finished before moving to the next file
					<-enqueueDone
					break
				}
				continue
			}
			// fmt.Println("Segment " + strconv.Itoa(expected) + " unexpected, saving to map")
			segmentMap[segment.Article.Number] = segment
		}
	}
	close(jobs)
	fmt.Println("Download Complete!")
}

func manager() {
	/*
		load config and define parameters
	*/
	config, err := loadConfig()
	if err != nil {
		fmt.Print("Error parsing config")
		return
	}
	maxConnections := config.Connections
	fmt.Print("Max Connections: " + strconv.Itoa(maxConnections) + "\n")
	/*
		make job pool and send maxConnections into pool to be multiplexed by workers
	*/
	connections := make(chan *tls.Conn, 20)
	for c := 1; c <= maxConnections; c++ {
		connection, err := NNTP.Connect(config)
		if err != nil {
			fmt.Print(err)
			return
		}
		fmt.Printf("Connection %d established\n", c)
		connections <- connection
	}
	/*
		load NZB file(s) from disk
	*/
	fmt.Print("Loading next nzb file...")
	b, err := os.ReadFile("test.nzb") // just pass the file name
	if err != nil {
		panic(err)
	}
	fmt.Println("Successfully Opened test.nzb")
	nzb, err := NZB.NewString(string(b)) // marshal, returning pointer to nzb object
	if err != nil {
		panic(err)
	}
	/*
		call download for each NZB opened
	*/
	go download(nzb, 0, 0, connections, maxConnections)

}

func main() {

	log.SetFlags(log.Lshortfile)

	go manager()

	select {} // block forever

}
