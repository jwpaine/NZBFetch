/*
~~~~rfc3977~~~~~
100 help text follows
   199 debug output
   200 server ready - posting allowed
   201 server ready - no posting allowed
   202 slave status noted
   205 closing connection - goodbye!
   211 n f l s group selected
   215 list of newsgroups follows
   220 n <a> article retrieved - head and body follow 221 n <a> article
   retrieved - head follows
   222 n <a> article retrieved - body follows
   223 n <a> article retrieved - request text separately 230 list of new
   articles by message-id follows
   231 list of new newsgroups follows
   235 article transferred ok
   240 article posted ok
   335 send article to be transferred.  End with <CR-LF>.<CR-LF>
   340 send article to be posted. End with <CR-LF>.<CR-LF>
   400 service discontinued
   411 no such news group
   412 no newsgroup has been selected
   420 no current article has been selected
   421 no next article in this group
   422 no previous article in this group
   423 no such article number in this group
   430 no such article found
   435 article not wanted - do not send it
   436 transfer failed - try again later
   437 article rejected - do not try again.
   440 posting not allowed
   441 posting failed
   500 command not recognized
   501 command syntax error
   502 access restriction or permission denied
   503 program fault - command not performed
*/

package main

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"strings"
)

type Segment struct {
	Article    NzbSegment // meta data from NZB
	Data       []byte     // data after download
	Connection *tls.Conn
	Groups     []string
}

func authenticate(username string, password string, conn *tls.Conn) (n int, err error) {
	n, err = send("AUTHINFO USER "+username, conn)
	if err != nil {
		return n, err
	}
	n, err = send("AUTHINFO PASS "+password, conn)
	if err != nil {
		return n, err
	}
	return
}
func connect(config Config) (conn *tls.Conn) {
	conf := &tls.Config{
	//InsecureSkipVerify: true,
	}
	/*
		open tcp connection to server
	*/
	conn, err := tls.Dial("tcp", config.Address+":"+config.Port, conf)
	if err != nil {
		log.Println(conn, err)
		return
	}
	// wait for server to be ready (STATUS CODE 200) and Authenticated (STATUS 281)
	for {
		// read message from server
		buf := make([]byte, 100)
		n, err := conn.Read(buf)
		if err != nil {
			conn = nil
			return
		}
		// tokenize message by space
		tokens := strings.Fields(string(buf[:n]))
		// if ready
		if tokens[0] == "200" || tokens[0] == "201" {
			fmt.Print("Server ready\n")
			// authenticate user
			n, err := authenticate(config.Username, config.Password, conn)
			if err != nil {
				log.Println(n, err)
				conn = nil
				return
			}
		}
		if tokens[0] == "502" {
			fmt.Print("Login failed\n")
			conn = nil
			return
		}

		if tokens[0] == "281" {
			fmt.Print("Login Success!\n")
			return
		}
	}

}

func fetchSegment(segment Segment) (Segment, error) {

	segmentId := segment.Article.Id
	readBuf := make([]byte, segment.Article.Bytes/2)
	segmentBuf := []byte("")
	readCount := 0
	conn := segment.Connection
	//	fmt.Print("Fetching segment: " + segmentId + "Size: " + strconv.Itoa(segment.Article.Bytes) + "\n")
	// try group n if segment missing from group n-1
	for i := 0; i < len(segment.Groups); i++ {
		group := string(segment.Groups[i])
		//	fmt.Print("Trying group: " + group + "\n")
		_, err := send("GROUP "+group, conn)
		if err != nil {
			return segment, err
		}
		// start reading and responding
		for {
			n, err := conn.Read(readBuf)
			if err != nil {
				fmt.Print(err)
				conn = nil
				return segment, err
			}
			// switch based on status code in reply from server
			status := strings.Fields(string(readBuf[:n]))[0]
			//	fmt.Print(string(readBuf[:n]))
			switch status {
			case "211": // group selected
				// get article
				_, err := send("BODY <"+segmentId+">", conn)
				if err != nil {
					return segment, err
				}
				continue
			case "411": // no such group
				break
			case "222": // Head and Body follow
				segmentBuf = append(segmentBuf, readBuf[:n]...) // append readBuf to segment
				// if end of file
				if bytes.Contains(readBuf, []byte("=yend")) {
					return Segment{segment.Article, segmentBuf, nil, nil}, nil
				}
				continue
			case "430":
				fmt.Print("430 no such article found\n")
				break
			default:
				// prior status was 220, or segment data so save
				readCount += n
				segmentBuf = append(segmentBuf, readBuf[:n]...) // append readBuf to segment
				// if end of segment found, return segmentBuf containing entire segment
				if bytes.Contains(readBuf, []byte("=yend")) {
					return Segment{segment.Article, segmentBuf, nil, nil}, nil
				}
				continue
			}
			break
		}
	}
	return segment, errors.New("Segment not found in any group")
}

func send(message string, conn *tls.Conn) (n int, err error) {
	return conn.Write([]byte(message + "\r\n"))
}
