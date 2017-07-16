package main

import (
	"github.com/jonas747/discordgo"
	"testing"
)

var (
	Silence = []byte{0xF8, 0xFF, 0xFE}
)

type DummyOutput struct {
}

func (d *DummyOutput) WriteOpus(data []byte) error {
	return nil
}

type ProxyOutput struct {
	ProxyChannel chan []byte
}

func (p *ProxyOutput) WriteOpus(data []byte) error {
	p.ProxyChannel <- data
	return nil
}

func TestUserDecoder(t *testing.T) {
	ud := NewUserDecoder(1)
	err := ud.HandlePacket(&discordgo.Packet{
		SSRC: 1,
		Opus: Silence,
	})

	if err != nil {
		t.Error("Error handling packet: ", err)
	}

	if len(ud.buf) != 960*2 {
		t.Error("Decoded size is not 960*2: ", len(ud.buf))
	}
}

func BenchmarkUserDecodeRead(b *testing.B) {
	ud := NewUserDecoder(1)
	p := &discordgo.Packet{
		SSRC: 1,
		Opus: Silence,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := ud.HandlePacket(p)
		if err != nil {
			b.Error("Failed handling packet: ", err)
			continue
		}

		buf := make([]int16, 960*2)
		n, _ := ud.Read(buf)
		if n != 960*2 {
			b.Error("Packet size is not 960*2: ", n)
			continue
		}
	}
}
