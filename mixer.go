package main

import (
	"github.com/hraban/opus"
	"github.com/jonas747/discordgo"
	"github.com/jonas747/opusutil"
	"github.com/pkg/errors"
	"sync"
	"time"
)

// UserDecoder represents a individual user's audio stream.
// TODO: Optimise UserDecoder to reuse the buffers
type UserDecoder struct {
	SSRC uint32

	decoder *opus.Decoder

	bufLock sync.Mutex
	buf     []int16
}

// NewUserDecoder Creates a new user UserDecoder, using the provided ssrc
func NewUserDecoder(ssrc uint32) *UserDecoder {
	dec, err := opus.NewDecoder(48000, 2)
	if err != nil {
		panic("Failed creating decoder: " + err.Error())
	}

	return &UserDecoder{
		decoder: dec,
		SSRC:    ssrc,
	}
}

// Handles an incoming voice packet
func (ud *UserDecoder) HandlePacket(packet *discordgo.Packet) error {

	header, err := opusutil.DecodeHeader(packet.Opus)
	if err != nil {
		return errors.WithMessage(err, "ud.HandlePacket, opusutil.DecodeHeader")
	}

	// Example: 1x 20000us frame at 48k = 1 * 20 * 48 * 2(channels) = 960 * 2 channels
	samples := int(float64(header.NumFrames)*float64(header.Config.FrameDuration.Seconds()*1000)*48) * 2

	pcm := make([]int16, samples)
	_, err = ud.decoder.Decode(packet.Opus, pcm)
	if err != nil {
		return errors.WithMessage(err, "ud.HandlePacket, ud.decoder.Decode")
	}

	ud.bufLock.Lock()
	ud.buf = append(ud.buf, pcm...)
	// log(packet.SSRC, ": Samples: ", samples, " bufsize: ", len(ud.buf))
	ud.bufLock.Unlock()

	return nil
}

// Read Implements io.Read, err is always nil as reads can always be performed
// TODO: Reads on users that has left the channel should return io.EOF
func (ud *UserDecoder) Read(b []int16) (n int, err error) {
	ud.bufLock.Lock()
	if len(ud.buf) < 1 {
		ud.bufLock.Unlock()
		return 0, nil
	}

	n = copy(b, ud.buf)
	ud.buf = ud.buf[n:]
	ud.bufLock.Unlock()
	return
}

// The mixer uotputs to mixerouputs
type MixerOutput interface {

	// WriteOpus gets called every 20ms with the next batch of opus data
	// If an error is returned, output is removed
	WriteOpus(opus []byte) error
}

// Mixer is the main DiscordRadio mixer, in charge of combining all streams
// and broadcastign them to all outputs
type Mixer struct {
	usersLock sync.Mutex
	users     map[uint32]*UserDecoder
	stop      chan bool

	volumeMultipliers map[uint32]float32

	encoder *opus.Encoder

	pcmbuf []int16

	outputLock sync.Mutex
	outputs    []MixerOutput
}

// NewMixer returns a new mixer with default values
func NewMixer() *Mixer {
	enc, err := opus.NewEncoder(48000, 2, opus.AppAudio)
	if err != nil {
		panic("Failed creating encoder: " + err.Error())
	}

	return &Mixer{
		stop:              make(chan bool),
		users:             make(map[uint32]*UserDecoder),
		volumeMultipliers: make(map[uint32]float32),
		encoder:           enc,
	}
}

func (mix *Mixer) SetVolume(ssrc uint32, volume float32) {
	mix.usersLock.Lock()
	mix.volumeMultipliers[ssrc] = volume
	mix.usersLock.Unlock()
}

// AddOutput Adds a new output to the mixer, which will then further receive mixed audio
// Every 20mx (Even if there are no people talking in the channel)
func (mix *Mixer) AddOutput(output MixerOutput) {
	mix.outputLock.Lock()
	mix.outputs = append(mix.outputs, output)
	mix.outputLock.Unlock()
}

// RemoveOutput removes an output from the mixer
func (mix *Mixer) RemoveOutput(output MixerOutput) {
	mix.outputLock.Lock()
	for k, v := range mix.outputs {
		if v == output {
			mix.outputs = append(mix.outputs[:k], mix.outputs[k+1:]...)
			break
		}
	}
	mix.outputLock.Unlock()
}

func (mix *Mixer) Stop() {
	close(mix.stop)
}

func (mix *Mixer) Queue(packet *discordgo.Packet) {

	st, ok := mix.users[packet.SSRC]
	if !ok {
		st = NewUserDecoder(packet.SSRC)
		mix.usersLock.Lock()
		mix.users[packet.SSRC] = st
		mix.usersLock.Unlock()
	}

	err := st.HandlePacket(packet)
	if err != nil {
		log("Error handling voice packet: ", err)
	}
}

func (mix *Mixer) Run() {
	log("Mixer running")
	ticker := time.NewTicker(time.Millisecond * 20)
	for {
		select {
		case <-mix.stop:
			log("Mixer stopping")
			return
		case <-ticker.C:
			mix.processQueue()
		}
	}
}

func (mix *Mixer) processQueue() {

	// log("Processing audio")
	// started := time.Now()

	mix.usersLock.Lock()
	mixedPCM := make([]int16, 48*20*2)
	for _, st := range mix.users {

		userPCM := make([]int16, 48*20*2)
		n, _ := st.Read(userPCM)
		if n < 1 {
			continue
		}

		mult, ok := mix.volumeMultipliers[st.SSRC]
		if !ok {
			mult = 1
		}

		for i := 0; i < len(userPCM); i++ {
			// Mix it

			v := int32(mixedPCM[i]) + int32(float32(userPCM[i])*mult)
			// Clip
			if v > 0x7fff {
				v = 0x7fff
			} else if v < -0x7fff {
				v = -0x7fff
			}
			mixedPCM[i] = int16(v)
		}
	}

	// log("Took ", time.Since(started), " To process queue")
	mix.usersLock.Unlock()

	output := make([]byte, 0xfff)
	n, err := mix.encoder.Encode(mixedPCM, output)
	if err != nil {
		log("Failed encode: ", err)
	}

	mix.broadcastAudio(output[:n])
}

func (mix *Mixer) broadcastAudio(opus []byte) {
	mix.outputLock.Lock()
	for _, output := range mix.outputs {
		go mix.sendToOutput(output, opus)
	}
	mix.outputLock.Unlock()
}

func (mix *Mixer) sendToOutput(output MixerOutput, audio []byte) {
	err := output.WriteOpus(audio)
	if err != nil {
		mix.RemoveOutput(output)
		log("Failed sending to output: ", err)
	}
}

// func RunEcho(s *discordgo.Session) {
// 	done := make(chan *sync.WaitGroup)
// 	runningLock.Lock()
// 	runningChannels = append(runningChannels, done)
// 	runningLock.Unlock()

// 	voice, err := s.ChannelVoiceJoin(GuildID, ChannelID, false, false)
// 	if err != nil {
// 		log("Voice err:", err)
// 		return
// 	}

// 	log("Waiting for voice")
// 	for !voice.Ready {
// 		time.Sleep(time.Millisecond)
// 	}
// 	log("Done waiting for voice")

// 	encoder := NewEncoder()
// 	encoder.vc = voice
// 	go encoder.Run()
// 	for {
// 		select {
// 		case packet := <-voice.OpusRecv:

// 			encoder.Queue(packet)
// 		case wg := <-done:
// 			voice.Close()

// 			encoder.Stop()

// 			wg.Done()
// 			return
// 		}
// 	}
// }
