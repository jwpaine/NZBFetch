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
	conn, err := tls.Dial("tcp", config.Address+":"+config.Port, conf)
	if err != nil {
		log.Println(conn, err)
		return nil, err
	}

	for {
		buf := make([]byte, 100)
		n, err := conn.Read(buf)
		if err != nil {
			return nil, err
		}
		tokens := strings.Fields(string(buf[:n]))
		if len(tokens) == 0 {
			continue
		}

		switch tokens[0] {
		case "200", "201":
			n, err := authenticate(config.Username, config.Password, conn)
			if err != nil {
				log.Println(n, err)
				return nil, err
			}
		case "502":
			return nil, errors.New("authentication failed")
		case "281":
			return conn, nil
		}
	}
}

func FetchSegment(segment Types.Segment) (Types.Segment, error) {
	segmentId := segment.Article.Id
	conn := segment.Connection
	readBuf := make([]byte, 4096)
	segmentBuf := make([]byte, 0, segment.Article.Bytes)

	for _, group := range segment.Groups {
		_, err := send("GROUP "+group, conn)
		if err != nil {
			return segment, err
		}

		for {
			n, err := conn.Read(readBuf)
			if err != nil {
				return segment, err
			}
			statusFields := strings.Fields(string(readBuf[:n]))
			if len(statusFields) > 0 {
				status := statusFields[0]
				switch status {
				case "211":
					_, err := send("BODY <"+segmentId+">", conn)
					if err != nil {
						return segment, err
					}
					continue
				case "411":
					fmt.Println("No such group: " + group)
					continue
				case "430":
					fmt.Println("430 no such article found")
					continue
				}
			}

			// If body line starts
			if bytes.Contains(readBuf, []byte("=ybegin")) {
				// Start of yEnc body
				bodyBuf := readBuf
				segmentBuf, err = readBodyOnTheFly(conn, bodyBuf)
				if err != nil {
					return segment, err
				}
				return Types.Segment{Article: segment.Article, Data: segmentBuf}, nil
			}
		}
	}

	return segment, errors.New("segment not found in any group")
}

// readBodyOnTheFly reads NNTP body and undot-stuffs as data arrives.
// Stops when terminator "." line or "=yend" marker found.
func readBodyOnTheFly(conn *tls.Conn, initial []byte) ([]byte, error) {
	buf := make([]byte, 4096)
	out := make([]byte, 0, len(initial)+4096)
	atLineStart := true
	prevCR := false

	// process initial buffer (could contain partial body)
	out = append(out, processNNTPChunk(initial, &atLineStart, &prevCR)...)

	for {
		n, err := conn.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			out = append(out, processNNTPChunk(chunk, &atLineStart, &prevCR)...)
			// detect =yend marker
			if bytes.Contains(out, []byte("=yend")) {
				break
			}
		}
		if err != nil {
			if strings.Contains(err.Error(), "EOF") {
				break
			}
			return out, err
		}
	}

	return out, nil
}

// processNNTPChunk performs on-the-fly undot-stuffing of a data chunk.
func processNNTPChunk(b []byte, atLineStart *bool, prevCR *bool) []byte {
	var out []byte
	for i := 0; i < len(b); i++ {
		c := b[i]

		// newline detection
		if c == '\r' {
			*prevCR = true
			out = append(out, c)
			continue
		}
		if c == '\n' {
			out = append(out, c)
			*atLineStart = true
			*prevCR = false
			continue
		}

		if *atLineStart && c == '.' {
			// Possible terminator or dot-stuffed line
			if i+1 < len(b) {
				nc := b[i+1]
				if nc == '.' {
					out = append(out, '.') // ".." -> "."
					i++
					*atLineStart = false
					continue
				} else if nc == '\r' || nc == '\n' {
					// Terminator "." line
					return out
				}
			}
		}

		out = append(out, c)
		*atLineStart = false
	}
	return out
}

func send(message string, conn *tls.Conn) (n int, err error) {
	return conn.Write([]byte(message + "\r\n"))
}
