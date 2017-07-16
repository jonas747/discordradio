package main

import (
	"github.com/jonas747/discordgo"
	"github.com/pkg/errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrGuildHostTaken    = errors.New("Server has a station")
	ErrGuildReceiveTaken = errors.New("Server has a receiver")
	ErrNameTaken         = errors.New("Name taken")
)

var (
	// Lock for ActiveStations and ActiveGuilds
	ActiveLock sync.RWMutex

	// List of all active stations
	ActiveStations []*Station

	// Maps guilds top stations
	ActiveGuilds = make(map[string]*Station)
)

type StationMeta struct {
	Name          string
	Description   string
	GuildID       string
	GuildName     string
	Host          *discordgo.User
	TextChannelID string
	Listeners     []*Listener
}

type Station struct {
	sync.RWMutex

	queuedSetVolumes map[string]float32
	meta             *StationMeta
	mixer            *Mixer

	stop chan bool
	vc   *discordgo.VoiceConnection
}

// FindStation searches for a station by name, or if the name is contained in the stations name with only 1 result
func FindStation(name string) *Station {
	name = strings.ToLower(name)

	ActiveLock.RLock()

	okayMatches := make([]*Station, 0)
	for _, station := range ActiveStations {
		stLower := strings.ToLower(station.meta.Name)
		if stLower == name {
			// Exact match, return immediately
			ActiveLock.RUnlock()
			return station
		}

		if strings.Contains(stLower, name) {
			okayMatches = append(okayMatches, station)
		}
	}
	ActiveLock.RUnlock()

	if len(okayMatches) == 1 {
		return okayMatches[0]
	}

	return nil
}

func StartStation(name, description string, guild *discordgo.Guild, textChannelID, voiceChannelID string, host *discordgo.User) (*Station, error) {
	ActiveLock.Lock()

	// Check if this guild has a receiver or station already
	if s, ok := ActiveGuilds[guild.ID]; ok {
		ActiveLock.Unlock()
		if s.meta.GuildID == guild.ID {
			return nil, ErrGuildHostTaken
		}

		return nil, ErrGuildReceiveTaken
	}

	station := &Station{
		meta: &StationMeta{
			Name:          name,
			Description:   description,
			GuildID:       guild.ID,
			GuildName:     guild.Name,
			Host:          host,
			TextChannelID: textChannelID,
		},
		stop:             make(chan bool),
		queuedSetVolumes: make(map[string]float32),
		mixer:            NewMixer(),
	}

	ActiveStations = append(ActiveStations, station)
	ActiveGuilds[guild.ID] = station
	ActiveLock.Unlock()

	err := station.Start(voiceChannelID)
	if err != nil {
		removeStation(guild.ID)
		return nil, errors.WithMessage(err, "StartStation")
	}
	return station, nil
}

func removeStation(guildID string) {
	ActiveLock.Lock()
	delete(ActiveGuilds, guildID)
	for k, v := range ActiveStations {
		if v.meta.GuildID == guildID {
			ActiveStations = append(ActiveStations[:k], ActiveStations[k+1:]...)
		}
	}
	ActiveLock.Unlock()
}

func (s *Station) Start(voiceChannelId string) error {
	vc, err := DG.ChannelVoiceJoin(s.meta.GuildID, voiceChannelId, true, false)
	// vc.LogLevel = discordgo.LogDebug
	if err != nil {
		return err
	}

	vc.AddHandler(s.VoiceSpeakingUpdateHandler)

	for !vc.Ready {
		time.Sleep(time.Millisecond * 10)
	}
	s.vc = vc

	go s.voiceRecv()
	go s.mixer.Run()
	return nil
}

func (s *Station) VoiceSpeakingUpdateHandler(vc *discordgo.VoiceConnection, vs *discordgo.VoiceSpeakingUpdate) {
	// log("Vocie updt my dude")
	// s.Lock()
	// if vol, ok := s.queuedSetVolumes[vs.UserID]; ok {
	// 	delete(s.queuedSetVolumes, vs.UserID)
	// 	s.Unlock()
	// 	log("Setting volume of ", uint32(vs.SSRC), ", ", vs.SSRC, " to ", vol)
	// 	s.mixer.SetVolume(uint32(vs.SSRC), vol)
	// } else {
	// 	s.Unlock()
	// }
}

func (s *Station) Stop() {
	close(s.stop)
}

// Status returns the stations meta info
func (s *Station) Meta() *StationMeta {
	s.RLock()
	mCop := new(StationMeta)
	*mCop = *s.meta

	mCop.Listeners = make([]*Listener, len(s.meta.Listeners))
	copy(mCop.Listeners, s.meta.Listeners)

	s.RUnlock()

	return mCop
}

// ListenIn listens in on the station from a voice channel
func (s *Station) ListenIn(guildID, voiceChannelID string, textChannelID string) (*Listener, error) {
	ActiveLock.Lock()
	if existing, ok := ActiveGuilds[guildID]; ok {
		ActiveLock.Unlock()
		if existing.Meta().GuildID == guildID {
			return nil, ErrGuildHostTaken
		}

		return nil, ErrGuildReceiveTaken
	}

	listener := &Listener{
		TextChannelID: textChannelID,
		GuildID:       guildID,
		station:       s,
		stop:          make(chan bool),
	}

	ActiveGuilds[guildID] = s
	ActiveLock.Unlock()

	err := listener.Start(voiceChannelID)
	if err != nil {
		ActiveLock.Lock()
		delete(ActiveGuilds, guildID)
		ActiveLock.Unlock()
		return nil, errors.WithMessage(err, "ListenIn")
	}

	s.Lock()
	s.meta.Listeners = append(s.meta.Listeners, listener)
	s.Unlock()

	s.mixer.AddOutput(listener)

	return listener, nil
}

// TODO stuff
func (s *Station) voiceRecv() {
	for {
		select {
		case opus := <-s.vc.OpusRecv:
			s.mixer.Queue(opus)
		case <-s.stop:
			s.shutDown()
		}
	}
}

func (s *Station) shutDown() {
	s.vc.Disconnect()

	s.Lock()
	for _, v := range s.meta.Listeners {
		v.Stop()
	}
	s.Unlock()

	removeStation(s.meta.GuildID)
}

func (s *Station) RemoveListenerByID(guildID string) {
	s.RLock()
	for _, v := range s.meta.Listeners {
		if v.GuildID == guildID {
			s.RUnlock()
			s.RemoveListener(v)
			return
		}
	}
	s.RUnlock()
}

func (s *Station) RemoveListener(l *Listener) {
	s.mixer.RemoveOutput(l)

	s.Lock()
	for k, v := range s.meta.Listeners {
		if v == l {
			s.meta.Listeners = append(s.meta.Listeners[:k], s.meta.Listeners[k+1:]...)
		}
	}
	s.Unlock()

	ActiveLock.Lock()
	delete(ActiveGuilds, l.GuildID)
	ActiveLock.Unlock()
}
