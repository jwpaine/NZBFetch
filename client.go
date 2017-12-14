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

func worker(id int, jobs <-chan Segment, con <-chan *tls.Conn, config Config, results chan<- Segment) {

	/*	var conn *tls.Conn
		conn = connect(config)
		if conn == nil {
			return
		}
		defer conn.Close()
		j.Connection = conn
	*/
	for c := range con {

		for j := range jobs {

			j.Connection = c
			//		fmt.Println("worker", id, "started  job", j.Article.Number)
			//	time.Sleep(time.Second)
			segment, err := fetchSegment(j)
			//		fmt.Println("worker", id, "finished job", j.Article.Number)
			// close connection
			//	c.Close()

			if err != nil {
				fmt.Print(err)
			}
			results <- segment

		}

	}
}

func segmentWriter(results chan Segment) {

	for {
		segment := <-results
		//	fmt.Println("Writing segment: " + strconv.Itoa(segment.Article.Number))
		fmt.Println("Writing segment: " + segment.Article.Id)
		// save segment
		//	fmt.Println("Success. Received segment of length: " + strconv.Itoa(len(segment)))
		// write segment to disk
		err := ioutil.WriteFile("test.yenc", segment.Data, 0644)
		if err != nil {
			fmt.Print(err)
			panic(err)
		}
		// decode
		f, err := os.Open("test.yenc")
		if err != nil {
			fmt.Print(err)
			break
		}
		part, err := yenc.Decode(f)
		if err != nil {
			fmt.Print(err)
			break
		}
		fmt.Println("Successful Decode: " + string(part.Name))
		/*	// write decoded part to disk
			// if file does not exist, create it
			if _, err := os.Stat(string(part.Name)); os.IsNotExist(err) {
				_, err := os.Create(string(part.Name))
				if err != nil {
					panic(err)
				}
			}
			// open file
			out, err := os.OpenFile(string(part.Name), os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				panic(err)
			}
			defer f.Close()
			out.Write(part.Body) */
	}
}

func write(segment Segment) {
	// write yenc to disk
	err := ioutil.WriteFile("test.yenc", segment.Data, 0644)
	if err != nil {
		fmt.Print(err)
		return
	}
	// decode
	f, err := os.Open("test.yenc")
	if err != nil {
		fmt.Print(err)
		return

	}
	part, err := yenc.Decode(f)
	if err != nil {
		fmt.Print(err)
	}
	//	fmt.Println("Successful Decode: " + string(part.Name))
	// write decoded part to disk
	// if file does not exist, create it
	if _, err := os.Stat(string(part.Name)); os.IsNotExist(err) {
		_, err := os.Create(string(part.Name))
		if err != nil {
			panic(err)
		}
	}
	// open file
	out, err := os.OpenFile(string(part.Name), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	out.Write(part.Body)

	fmt.Print("Written")
}

func manager() {
	// load config ===============
	config, err := loadConfig()
	if err != nil {
		fmt.Print("Error parsing config")
		return
	}
	maxConnections := config.Connections
	fmt.Print("Max Connections: " + strconv.Itoa(maxConnections) + "\n")

	connections := make(chan *tls.Conn, 20)
	jobs := make(chan Segment, 200)
	results := make(chan Segment, 100)

	for w := 1; w <= 20; w++ { // 3 connections
		go worker(w, jobs, connections, config, results)
	}

	for c := 1; c <= 5; c++ {
		connections <- connect(config)
	}

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
	//	go segmentWriter(results)

	// for each file in nzb
	for i := 0; i < len(nzb.Files); i++ {
		// create map to keep track of out-of-order segments
		segmentMap := make(map[int]Segment)
		var expected = 1
		// for each segment
		fmt.Println("Working on new File: " + nzb.Files[i].Subject)
		// add each segment to jobs pool
		for j := 0; j < len(nzb.Files[i].Segments); j++ {
			jobs <- Segment{nzb.Files[i].Segments[j], nil, nil, nzb.Files[i].Groups}
		}
		for {
			//	fmt.Println("loop start")
			segment := <-results
			fmt.Println("Got segment: " + segment.Article.Id)
			//	fmt.Println("Expecting value: " + strconv.Itoa(expected))
			//	fmt.Println("Writing segment: " + segment.Article.Id)
			// save segment
			//	fmt.Println("Success. Received segment of length: " + strconv.Itoa(len(segment)))
			// write segment to disk

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

			/*	// write decoded part to disk
				// if file does not exist, create it
				if _, err := os.Stat(string(part.Name)); os.IsNotExist(err) {
					_, err := os.Create(string(part.Name))
					if err != nil {
						panic(err)
					}
				}
				// open file
				out, err := os.OpenFile(string(part.Name), os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					panic(err)
				}
				defer f.Close()
				out.Write(part.Body) */
		}
	}
	close(jobs)
	// get total segments we should listen for instead of defining number
	/*	for a := 1; a <= 10; a++ {
		segment := <-results
		fmt.Print("Writing segment: " + strconv.Itoa(segment.Article.Number))

	} */

	// fetch articles ===========================
	// for each file in nzb
	/*	for i := 0; i < len(nzb.Files); i++ {
			// for each segment
			fmt.Println("Working on File: " + nzb.Files[i].Subject)
			for j := 0; j < len(nzb.Files[i].Segments); j++ {
				segment, err := fetchSegment(nzb.Files[i].Segments[j], nzb.Files[i].Groups, conn)
				if err != nil {
					fmt.Print(err)
					continue
				}
				// save segment
				//	fmt.Println("Success. Received segment of length: " + strconv.Itoa(len(segment)))
				// write segment to disk
				err = ioutil.WriteFile("test.yenc", segment, 0644)
				if err != nil {
					fmt.Print(err)
					panic(err)
				}
				// decode
				f, err := os.Open("test.yenc")
				if err != nil {
					fmt.Print(err)
					break
				}
				part, err := yenc.Decode(f)
				if err != nil {
					fmt.Print(err)
					break
				}
				//	fmt.Println("Successful Decode: " + string(part.Name))
				// write decoded part to disk
				// if file does not exist, create it
				if _, err := os.Stat(string(part.Name)); os.IsNotExist(err) {
					_, err := os.Create(string(part.Name))
					if err != nil {
						panic(err)
					}
				}
				// open file
				out, err := os.OpenFile(string(part.Name), os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					panic(err)
				}
				defer f.Close()
				out.Write(part.Body)
			}
		}
	*/
	fmt.Println("Download Complete!")
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
}
