package main

import (
	"bufio"
	"crypto/tls"
	"fmt"

	"io/ioutil"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml" // config as Tom's Obvious, Minimal Language
	"github.com/chrisfarms/yenc"
	//	"gopkg.in/yenc.v0"
	// decode yenc
)

type Config struct {
	Address     string
	Port        string
	Secure      string
	Username    string
	Password    string
	Connections int
}

func loadConfig() (conf Config, err error) {
	b, err := ioutil.ReadFile("client.conf") // just pass the file name
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
func worker(id int, jobs <-chan Segment, con <-chan *tls.Conn, results chan<- Segment) {
	for c := range con {
		for j := range jobs {
			j.Connection = c
			segment, err := fetchSegment(j)
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
func write(segment Segment) {

	err := ioutil.WriteFile("test.yenc", segment.Data, 0644)
	if err != nil {
		fmt.Print(err)
		return
	}

	d, err := os.Open("test.yenc")
	if err != nil {
		fmt.Print(err)
		return
	}

	part, err := yenc.Decode(d)
	if err != nil {
		panic("decoding: " + err.Error())
	}
	fmt.Println("Decoded: Filename", part.Name)
	//	fmt.Println("Body Bytes", part.Body)

	// var b bytes.Buffer
	// write yenc to disk
	/*	err := ioutil.WriteFile("test.yenc", segment.Data, 0644)
			if err != nil {
				fmt.Print(err)
				return
			}

			dec, err := os.Open("test.yenc")
			if err != nil {
				fmt.Print(err)
				return
			}


		out, err := os.OpenFile(yread.Filename, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			fmt.Println("open " + yread.Filename + " Failed")
			return
		}

		fmt.Println("open " + yread.Filename + " SUCCESS")

		_, err = io.Copy(out, yread)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}

		fmt.Println("Successful write part: " + yread.Filename)


	*/

	// buf1, err := ioutil.ReadAll(yenc)

	/***********************/

	/* d, err := yenc.Decode(f) // , yenc.DecodeWithBufferSize(20)

	if err != nil {
		fmt.Print("error decoding segment")
		return
		//	panic(err)

	}

	fmt.Println("Decoded: " + d.Header().Name)

	_, err = io.Copy(&b, d)

	if err != nil {
		fmt.Print("error copying to memory")
		return
	}

	fmt.Print("Copied decoded to memory")

	*/

	/*
		_, err = io.Copy(&b, d)

		if err != nil {
			fmt.Print(err)
			return
		}

		fmt.Print("Copied decoded to memory")

		if _, err := os.Stat(d.Header().Name); os.IsNotExist(err) {
			_, err := os.Create(d.Header().Name)
			if err != nil {
				panic(err)
			}
		}

		out, err := os.OpenFile(d.Header().Name, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			panic(err)
		}

		fmt.Println("Opened file for writing: " + d.Header().Name)

		_, err = b.WriteTo(out)
		if err != nil {
			// Handle the error
			fmt.Println("Failed to write buffer to file:", err)
			return
		}

		fmt.Println("Buffer contents written to file successfully.")

	*/

	/*
		part, err := yenc.Decode(f)
		if err != nil {
			fmt.Print(err)
		}
		fmt.Println("Successful Decode: " + string(part.Header().Name))
		// write decoded part to disk
		// if file does not exist, create it
		if _, err := os.Stat(string(part.Header().Name)); os.IsNotExist(err) {
			_, err := os.Create(string(part.Header().Name))
			if err != nil {
				panic(err)
			}
		}
		// open file
		out, err := os.OpenFile(string(part.Header().Name), os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		out.Write(part.)
		fmt.Print("Written") */

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

/*
manage the download of files and segments contained in a single nzb file
*/
func download(nzb *Nzb, fileBegin int, segmentBegin int, connections chan *tls.Conn, maxWorkers int) {
	jobs := make(chan Segment, 200)
	results := make(chan Segment, 100)

	for w := 1; w <= maxWorkers; w++ { // 3 connections
		go worker(w, jobs, connections, results)
	}
	// for each file in nzb
	for i := fileBegin; i < len(nzb.Files); i++ {
		// create map to keep track of out-of-order segments
		segmentMap := make(map[int]Segment)
		var expected = 1
		// for each segment
		fmt.Println("Working on new File: " + nzb.Files[i].Subject)
		// add each segment to jobs pool
		for j := segmentBegin; j < len(nzb.Files[i].Segments); j++ {
			jobs <- Segment{nzb.Files[i].Segments[j], nil, nil, nzb.Files[i].Groups}
		}
		for {
			segment := <-results
			size := len(segment.Data)
			fmt.Println("Got segment: " + segment.Article.Id + " Article expected size (bytes): " + strconv.Itoa(segment.Article.Bytes))
			fmt.Printf("Actual Size of segment.Data: %d\n", size)

			if segment.Article.Number == expected {
				fmt.Println("Segment " + strconv.Itoa(expected) + " expected, writing to disk")
				write(segment)
				expected++
				// write segments stored in memory:
				for expected < len(nzb.Files[i].Segments)+1 {
					fmt.Println("Checking memory")
					j := segmentMap[expected]
					// if next segment not found in memory
					if j.Article.Number == 0 {
						fmt.Println("next segment not found in memory")
						break
					}
					// if found, write to disk
					fmt.Println("Found segment " + strconv.Itoa(expected) + " in memory, writing")
					write(j)
					delete(segmentMap, expected)
					expected++
				}
				// check if this is last segment
				if expected > len(nzb.Files[i].Segments) {
					fmt.Println("This is last segment")
					break
				}
				continue
			}
			fmt.Println("Segment " + strconv.Itoa(expected) + " unexpected, saving to map")
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
		connections <- connect(config)
	}
	/*
		load NZB file(s) from disk
	*/
	fmt.Print("Loading next nzb file...")
	b, err := ioutil.ReadFile("test.nzb") // just pass the file name
	if err != nil {
		panic(err)
	}
	fmt.Println("Successfully Opened test.nzb")
	nzb, err := NewString(string(b)) // marshal, returning pointer to nzb object
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
	scanner := bufio.NewScanner(os.Stdin)
	go manager()

	for scanner.Scan() {
		text := scanner.Text()
		tokens := strings.Fields(text)
		if tokens[0] == "/pause" {
			fmt.Print("Pausing all downloads \n")
		}
	}
	if scanner.Err() != nil {
		// handle error.
	}

	// test decode
	/*	f, err := os.Open("test.yenc")
		if err != nil {
			fmt.Print(err)
			return
		}
		d, err := yenc.Decode(f)
		if err != nil {
			fmt.Print(err)
		}

		fmt.Println("Successful Decode: " + d.Header().Name)
	*/

}
