package main

import (
	"github.com/jonas747/discordgo"
	"github.com/pkg/errors"
	"time"
)

var (
	ErrVoiceSendTimeout = errors.New("Timeout sending to voice")
	ErrVoiceClosed      = errors.New("Vocie is already closed")
)

type Listener struct {
	TextChannelID string
	GuildID       string

	stop    chan bool
	vc      *discordgo.VoiceConnection
	station *Station
}

func (l *Listener) Stop() {
	close(l.stop)
}

func (l *Listener) Start(voiceChannelID string) error {
	vc, err := DG.ChannelVoiceJoin(l.GuildID, voiceChannelID, false, true)
	if err != nil {
		return err
	}

	for !vc.Ready {
		time.Sleep(time.Millisecond * 10)
	}

	l.vc = vc
	return nil
}

func (l *Listener) WriteOpus(data []byte) error {
	select {
	case l.vc.OpusSend <- data:
		return nil
	case <-time.After(time.Second):
	case <-l.stop:
	}

	// If we timed out or we stopped, close the voice conn and remove the channel
	l.vc.Close()
	l.station.RemoveListener(l)

	return nil
}
