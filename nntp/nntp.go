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

package nntp

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	Types "nzbfetch/types"
	"strings"
)

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
func Connect(config Types.Config) (conn *tls.Conn, _ error) {
	conf := &tls.Config{
		InsecureSkipVerify: false,
	}
	/*
		open tcp connection to server
	*/
	conn, err := tls.Dial("tcp", config.Address+":"+config.Port, conf)
	if err != nil {
		log.Println(conn, err)
		return nil, err
	}
	// wait for server to be ready (STATUS CODE 200) and Authenticated (STATUS 281)
	for {
		// read message from server
		buf := make([]byte, 100)
		n, err := conn.Read(buf)
		if err != nil {
			return nil, err
		}
		// tokenize message by space
		tokens := strings.Fields(string(buf[:n]))
		// if ready
		if tokens[0] == "200" || tokens[0] == "201" {
			// fmt.Print("Server ready\n")
			// authenticate user
			n, err := authenticate(config.Username, config.Password, conn)
			if err != nil {
				log.Println(n, err)
				return nil, err
			}
		}
		if tokens[0] == "502" {
			fmt.Print("Login failed\n")
			return nil, errors.New("Authentication failed")
		}

		if tokens[0] == "281" {
			// login success
			return conn, nil
		}
	}

}

func FetchSegment(segment Types.Segment) (Types.Segment, error) {

	segmentId := segment.Article.Id
	readBuf := make([]byte, segment.Article.Bytes/2)
	segmentBuf := []byte("")
	conn := segment.Connection

	for i := 0; i < len(segment.Groups); i++ {
		group := string(segment.Groups[i])
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

			switch status {
			case "211": // group selected
				// get article
				_, err := send("BODY <"+segmentId+">", conn)
				if err != nil {
					return segment, err
				}
				continue
			case "411": // no such group
				fmt.Println("No such group: " + group)
				continue
			case "222": // Body follows
				// check for =ybegin
				startIndex := bytes.Index(readBuf, []byte("=ybegin"))

				// start found
				if startIndex != -1 {
					segmentBuf = append(segmentBuf, readBuf[startIndex:n]...) // Append readBuf from startIndex to n
				}
				// if we actually end here, return the segment...
				if bytes.Contains(readBuf, []byte("=yend")) {
					segmentBuf = undotStuff(segmentBuf)
					return Types.Segment{Article: segment.Article, Data: segmentBuf}, nil
				}
				continue
			case "430":
				fmt.Print("430 no such article found\n")
				continue
			default:
				// prior status was 220, or segment data so save

				startIndex := bytes.Index(readBuf, []byte("=ybegin"))
				// start found
				if startIndex != -1 {
					segmentBuf = append(segmentBuf, readBuf[startIndex:n]...) // Append readBuf from startIndex to n
					continue
				}

				segmentBuf = append(segmentBuf, readBuf[:n]...) // append readBuf to segment

				// return segment data if we find =yend
				if bytes.Contains(readBuf, []byte("=yend")) {
					segmentBuf = undotStuff(segmentBuf)
					return Types.Segment{segment.Article, segmentBuf, nil, nil}, nil
				}
			}

		}
	}
	return segment, errors.New("Segment not found in any group")
}

func send(message string, conn *tls.Conn) (n int, err error) {
	return conn.Write([]byte(message + "\r\n"))
}

// undotStuff removes NNTP dot-stuffing from the accumulated article body.
// It also drops a lone terminator line "."
func undotStuff(b []byte) []byte {
	var out = make([]byte, 0, len(b))
	atLineStart := true
	unstuffs := 0
	// terminatorDropped := false
	lines := 0

	for i := 0; i < len(b); i++ {
		c := b[i]

		// Handle CRLF and LF as newlines
		if c == '\r' {
			out = append(out, '\r')
			if i+1 < len(b) && b[i+1] == '\n' {
				out = append(out, '\n')
				i++
			}
			atLineStart = true
			lines++
			continue
		}
		if c == '\n' {
			out = append(out, '\n')
			atLineStart = true
			lines++
			continue
		}

		if atLineStart && c == '.' {
			// Check for NNTP terminator "." followed by EOL or end
			if i+1 == len(b) || b[i+1] == '\r' || b[i+1] == '\n' {
				break
			}
			// Dot-stuffed line: ".." -> emit single '.'
			if i+1 < len(b) && b[i+1] == '.' {
				out = append(out, '.')
				i++
				unstuffs++
				atLineStart = false
				continue
			}
		}

		out = append(out, c)
		atLineStart = false
	}

	return out
}
