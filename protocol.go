package proxylite

import (
	"encoding/binary"
	"errors"
	"net"
	"time"

	"github.com/sirupsen/logrus"
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

	TypeDataSegment = 64
)

// AskFreePortReq Ask available free ports request
type AskFreePortReq struct{}

// AskFreePortRsp Ask available free ports response
type AskFreePortRsp struct {
	Ports []uint32
}

// RegisterInfo Register information
type RegisterInfo struct {
	OuterPort uint32
	InnerAddr string
	Name      string
	Message   string
}

// ControlInfo Register Controlling information
type ControlInfo struct {
	MaxServeTime  uint32
	MaxServeConn  uint32
	MaxServeCount uint32
}

// RegisterServiceReq inner service registration request
type RegisterServiceReq struct {
	Info RegisterInfo
	Ctrl ControlInfo
}

const (
	RegisterRspOK = iota
	RegisterRspPortNotAllowed
	RegisterRspPortOccupied
	RegisterRspServerError
)

// RegisterServiceRsp inner service registration response
type RegisterServiceRsp struct {
	Code int32
}

// AskServiceReq Service discovery request
type AskServiceReq struct {
	Prefix string
}

// ServiceInfo Service basic information
type ServiceInfo struct {
	Port    uint32
	Name    string
	Message string
	Birth   time.Time

	Online       uint32    // online user count
	Capacity     uint32    // max concurrency user count
	AlreadyServe uint32    // already served user number
	TotalServe   uint32    // total user number can serve
	DeadLine     time.Time // time to close this port
}

// AskServiceRsp Service discovery response
type AskServiceRsp struct {
	Services []ServiceInfo
}

func writeUidUnsafe(buf []byte, uid uint32) {
	binary.LittleEndian.PutUint32(buf, uid)
}

func writeUidWithCloseUnsafe(buf []byte, uid uint32) {
	binary.LittleEndian.PutUint32(buf, uid)
	binary.LittleEndian.PutUint32(buf[4:], 886)
}

func readUidUnsafe(buf []byte) (uint32, bool) {
	uid := binary.LittleEndian.Uint32(buf)
	close := binary.LittleEndian.Uint32(buf[4:])
	return uid, close == 0
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

func sendMessageOnBuffer(conn net.Conn, mtype int, buffer []byte, dlen int) error {
	if len(buffer) < dlen+8 {
		return errors.New("send buffer too small")
	}

	binary.LittleEndian.PutUint32(buffer[0:], uint32(mtype))
	binary.LittleEndian.PutUint32(buffer[4:], uint32(dlen))

	Offset := 0
	for Offset < dlen+8 {
		n, err := conn.Write(buffer[Offset : dlen+8])
		if err != nil {
			return err
		}

		Offset += n
	}
	return nil
}

func recvMessageWithBuffer(conn net.Conn, buffer []byte) (int, []byte, error) {
	Len := 0
	if len(buffer) < 8 {
		return 0, nil, errors.New("recv buffer too small")
	}

	Offset := 0
	for Offset < 8 {
		n, err := conn.Read(buffer[Offset:8])
		if err != nil {
			return 0, nil, err
		}
		Offset += n
	}

	mtype := int(binary.LittleEndian.Uint32(buffer[0:]))
	Len = int(binary.LittleEndian.Uint32(buffer[4:]))
	if len(buffer) < Len+8 {
		logrus.Warnf("the given recv buffer length is %d while segment requires %d", len(buffer), Len+8)
		buffer = make([]byte, Len)
	} else {
		buffer = buffer[8:]
	}

	Offset = 0
	for Offset < Len {
		n, err := conn.Read(buffer[Offset:Len])
		if err != nil {
			return 0, nil, err
		}
		Offset += n
	}
	return mtype, buffer[:Len], nil
}
