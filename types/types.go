package types

import (
	"crypto/tls"
	NZB "nzbfetch/nzb"
)

type Config struct {
	Address     string
	Port        string
	Secure      string
	Username    string
	Password    string
	Connections int
}

type Segment struct {
	Article    NZB.NzbSegment // meta data from NZB
	Data       []byte         // data after download
	Connection *Connection
	Groups     []string
	GroupUsed  string
}

type Connection struct {
	Conn      *tls.Conn
	LastGroup string
}
