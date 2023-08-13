package main

import (
	"encoding/binary"
	"net"
	"time"
)

const (
	TypeMsgUndefined = iota
	TypeMsgMalformed
	TypeAskFreePortReq
	TypeAskFreePortRsp
	TypeAskServiceReq
	TypeAskServiceRsp
	TypeRegisterServiceReq
	TypeRegisterServiceRsp
)

type AskFreePortReq struct{}

type AskFreePortRsp struct {
	Ports []int
}

type RegisterInfo struct {
	OuterPort int
	InnerAddr string
	Name      string
	Message   string
}

type RegisterServiceReq struct {
	Info RegisterInfo
}

const (
	RegisterRspOK = iota
	RegisterRspPortNotAllowed
	RegisterRspPortOccupied
	RegisterRspServerError
)

type RegisterServiceRsp struct {
	Code int
}

type AskServiceReq struct {
	Prefix string
}

type ServiceInfo struct {
	Port    int
	Name    string
	Message string
	Busy    bool
	Birth   time.Time
}

type AskServiceRsp struct {
	Services []ServiceInfo
}

func sendMessage(conn net.Conn, mtype int, raw []byte) error {
	Len := len(raw)
	Buf := make([]byte, Len+8)
	binary.LittleEndian.PutUint32(Buf[0:], uint32(mtype))
	binary.LittleEndian.PutUint32(Buf[4:], uint32(Len))
	copy(Buf[8:], raw)

	Offset := 0
	for Offset < Len+8 {
		n, err := conn.Write(Buf[Offset:])
		if err != nil {
			return err
		}

		Offset += n
	}
	return nil
}

func recvMessage(conn net.Conn) (int, []byte, error) {
	Len := 0
	Buf := make([]byte, 8)

	Offset := 0
	for Offset < 8 {
		n, err := conn.Read(Buf[Offset:])
		if err != nil {
			return 0, nil, err
		}
		Offset += n
	}

	mtype := int(binary.LittleEndian.Uint32(Buf[0:]))
	Len = int(binary.LittleEndian.Uint32(Buf[4:]))

	Buf = make([]byte, Len)
	Offset = 0
	for Offset < Len {
		n, err := conn.Read(Buf[Offset:])
		if err != nil {
			return 0, nil, err
		}

		Offset += n
	}
	return mtype, Buf, nil
}
