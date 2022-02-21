package av

import (
	"fmt"
	"io"
)

const (
	TagAudio          = 0x08
	TagVideo          = 0x09
	TagScriptDataAmf0 = 0x12
	TagScriptDataAmf3 = 0x0f
)

const (
	SoundMp3 = 0x02
	SoundAac = 0x0a

	AacSeqHdr = 0x00
	AacRaw    = 0x01
)

const (
	AvcSeqHdr = 0x00

	FrameKey   = 0x01
	FrameInter = 0x02

	VideoH264 = 0x07
)

const (
	Publish = "publish"
	Play    = "play"
)

// Header can be converted to AudioHeaderInfo or VideoHeaderInfo
type Packet struct {
	IsAudio    bool
	IsVideo    bool
	IsMetadata bool
	TimeStamp  uint32 // dts
	StreamID   uint32
	Header     PacketHeader
	Data       []byte
}

type PacketHeader interface {
}

type AudioPacketHeader interface {
	PacketHeader
	SoundFormat() uint8
	AACPacketType() uint8
}

type VideoPacketHeader interface {
	PacketHeader
	IsKeyFrame() bool
	IsSeq() bool
	CodecID() uint8
	CompositionTime() int32
}

type Demuxer interface {
	Demux(*Packet) (ret *Packet, err error)
}

type Muxer interface {
	Mux(*Packet, io.Writer) error
}

type SampleRater interface {
	SampleRate() (int, error)
}

type CodecParser interface {
	SampleRater
	Parse(*Packet, io.Writer) error
}

type GetWriter interface {
	GetWriter(Info) WriteCloser
}

type Handler interface {
	HandleReader(ReadCloser)
	HandleWriter(WriteCloser)
}

type Alive interface {
	Alive() bool
}

type Closer interface {
	Info() Info
	Close(error)
}

type CalcTime interface {
	CalcBaseTimestamp()
}

type Info struct {
	Key   string
	URL   string
	UID   string
	Inter bool
}

func (info Info) IsInterval() bool {
	return info.Inter
}

func (info Info) String() string {
	return fmt.Sprintf("<key: %s, URL: %s, UID: %s, Inter: %v>",
		info.Key, info.URL, info.UID, info.Inter)
}

type ReadCloser interface {
	Closer
	Alive
	Read(*Packet) error
}

type WriteCloser interface {
	Closer
	Alive
	CalcTime
	Write(*Packet) error
}
