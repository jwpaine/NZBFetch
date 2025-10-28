package nntp

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	Types "nzbfetch/types"
	"strings"

	"bufio"
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

func Connect(config Types.Config) (*Types.Connection, error) {
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
			return &Types.Connection{
				Conn:      conn,
				LastGroup: "",
			}, nil
		}
	}
}

func FetchSegment(segment Types.Segment) (Types.Segment, error) {
	segmentId := segment.Article.Id
	conn := segment.Connection.Conn
	reader := bufio.NewReaderSize(conn, 256*1024)
	readBuf := make([]byte, 4096)
	segmentBuf := make([]byte, 0, segment.Article.Bytes)

	for _, group := range segment.Groups {
		// Only send GROUP if this connection isn't already in that group
		if segment.Connection.LastGroup != group {
			// fmt.Println("Switching to group:", group)
			if _, err := send("GROUP "+group, conn); err != nil {
				return segment, err
			}

			// Always read server response after sending GROUP
			n, err := reader.Read(readBuf)
			if err != nil {
				return segment, err
			}

			tokens := strings.Fields(string(readBuf[:n]))
			if len(tokens) == 0 {
				continue
			}

			switch tokens[0] {
			case "211":
				// fmt.Println("Group selected:", group)
				segment.Connection.LastGroup = group // ✅ update connection state
			case "411":
				fmt.Println("No such group:", group)
				continue
			default:
				continue
			}
		}

		// Request the article body
		if _, err := send("BODY <"+segmentId+">", conn); err != nil {
			return segment, err
		}

		// Expect "222 ... body follows" or "430 no such article"
		n, err := reader.Read(readBuf)
		if err != nil {
			return segment, err
		}

		statusFields := strings.Fields(string(readBuf[:n]))
		if len(statusFields) == 0 {
			continue
		}

		switch statusFields[0] {
		case "222":
			// fmt.Println("Downloading segment from:", group)
			segmentBuf, err = readBodyOnTheFly(reader)
			if err != nil {
				return segment, err
			}

			// Sanity check before returning
			if !bytes.Contains(segmentBuf, []byte("=ybegin")) {
				return segment, fmt.Errorf("no yEnc header found for %s", segmentId)
			}

			return Types.Segment{
				Article:   segment.Article,
				Data:      segmentBuf,
				GroupUsed: group,
			}, nil

		case "430":
			fmt.Println("430 no such article found in group:", group)
			continue

		default:
			continue
		}
	}

	return segment, errors.New("segment not found in any listed group")
}

// readBodyOnTheFly reads NNTP body and undot-stuffs as data arrives.

func readBodyOnTheFly(r *bufio.Reader) ([]byte, error) {
	var out bytes.Buffer

	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return out.Bytes(), err
		}

		// End-of-article marker: "." or ".\r\n"
		if bytes.Equal(line, []byte(".\r\n")) || bytes.Equal(line, []byte(".\n")) {
			break
		}

		// Dot-stuffed lines ("..") become single dot lines
		if len(line) > 1 && line[0] == '.' && line[1] == '.' {
			line = line[1:]
		}

		out.Write(line)

		// We still read until terminator even after yend
		if bytes.Contains(line, []byte("=yend")) {
			continue
		}
	}

	return out.Bytes(), nil
}

func send(message string, conn *tls.Conn) (n int, err error) {
	return conn.Write([]byte(message + "\r\n"))
}
