package main

import (
	"fmt"
	"github.com/jonas747/dcmd"
	"github.com/jonas747/discordgo"
)

func InitCommands(sys *dcmd.System) {
	sys.Root.AddCommand(dcmd.NewStdHelpCommand(), dcmd.NewTrigger("help", "h"))

	sys.Root.AddCommand(&dcmd.SimpleCmd{
		ShortDesc: "Start a broadcast",
		LongDesc:  "Start a broadcast in the current server with the specified name",
		RunFunc:   CmdStartBroadcast,
		CmdArgDefs: []*dcmd.ArgDef{
			&dcmd.ArgDef{Name: "Name", Type: dcmd.String},
		},
		RequiredArgDefs: 1,
	}, dcmd.NewTrigger("broadcast", "b"))

	sys.Root.AddCommand(&dcmd.SimpleCmd{
		ShortDesc: "Listen in to specified broadcast",
		LongDesc:  "Listen in to the specified broadcast by name",
		RunFunc:   CmdListen,
		CmdArgDefs: []*dcmd.ArgDef{
			&dcmd.ArgDef{Name: "Name", Type: dcmd.String},
		},
		RequiredArgDefs: 1,
	}, dcmd.NewTrigger("listen", "tunein", "l"))

	sys.Root.AddCommand(&dcmd.SimpleCmd{
		ShortDesc: "Stops the current broadcast or listening on to the broadcast",
		RunFunc:   CmdStop,
	}, dcmd.NewTrigger("stop", "leave"))

	sys.Root.AddCommand(&dcmd.SimpleCmd{
		ShortDesc: "Sets the volume of the specified user in percentage",
		RunFunc:   CmdVolume,
		CmdArgDefs: []*dcmd.ArgDef{
			&dcmd.ArgDef{Name: "User", Type: dcmd.UserReqMention},
			&dcmd.ArgDef{Name: "Volume", Type: &dcmd.FloatArg{Min: 0, Max: 200}},
		},
		RequiredArgDefs: 2,
	}, dcmd.NewTrigger("volume", "vol"))

	sys.Root.AddCommand(&dcmd.SimpleCmd{
		ShortDesc: "Lists all stations",
		RunFunc:   CmdListStations,
	}, dcmd.NewTrigger("stations", "list"))
}

func CmdStartBroadcast(d *dcmd.Data) (interface{}, error) {
	DG.State.RLock()
	vcID := FindUserVoiceChannel(d.Guild, d.Msg.Author.ID)
	DG.State.RUnlock()

	if vcID == "" {
		return "You have to be in a voice channel to start a broadcast", nil
	}

	_, err := StartStation(d.Args[0].Str(), "", d.Guild, d.Msg.ChannelID, vcID, d.Msg.Author)
	if err != nil {
		if err == ErrGuildHostTaken || err == ErrGuildReceiveTaken {
			return "There is already a station being broadcasted from here or listening in on a station.", nil
		}

		return err, err
	}

	return "Started a broadcast!\nThe notifications channel has been set to this one.", nil
}

func CmdListen(d *dcmd.Data) (interface{}, error) {
	DG.State.RLock()
	vcID := FindUserVoiceChannel(d.Guild, d.Msg.Author.ID)
	DG.State.RUnlock()

	if vcID == "" {
		return "You have to be in a voice channel to listen in to a station", nil
	}

	station := FindStation(d.Args[0].Str())
	if station == nil {
		return "No station found by that name, or the search had multiple matches, try to be more exact", nil
	}

	_, err := station.ListenIn(d.Guild.ID, vcID, d.Msg.ChannelID)
	if err != nil {
		if err == ErrGuildHostTaken || err == ErrGuildReceiveTaken {
			return "There is already a station being broadcasted from here or listening in on a station.", nil
		}

		return err, err
	}

	name := station.Meta().Name

	return "Tuned into " + name + ", The notifications channel has been set to this one.", nil
}

func CmdStop(d *dcmd.Data) (interface{}, error) {
	ActiveLock.Lock()
	st, ok := ActiveGuilds[d.Guild.ID]
	ActiveLock.Unlock()
	if !ok {
		return "No broadcast and no station tuned into from this channel", nil
	}

	if st.Meta().GuildID == d.Guild.ID {
		if st.Meta().Host.ID != d.Msg.Author.ID {
			return "Only the host of the broadcast can stop it", nil
		}
		// This is a broadcast
		st.Stop()
		return "Stopped the broadcast in this server", nil
	}

	st.RemoveListenerByID(d.Guild.ID)
	return "Stopped tuning into " + st.Meta().Name + ", have a nice day!", nil
}

func CmdVolume(d *dcmd.Data) (interface{}, error) {
	ActiveLock.Lock()
	st, ok := ActiveGuilds[d.Guild.ID]
	ActiveLock.Unlock()
	if !ok {
		return "No broadcast from this server", nil
	}

	if st.Meta().GuildID != d.Guild.ID {
		return "No broadcast from this server", nil
	}

	vol := d.Args[1].Value.(float64) / 100
	// st.Lock()
	// st.queuedSetVolumes[d.Args[0].Value.(*discordgo.User).ID] = float32(vol)
	// st.Unlock()

	st.vc.Lock()
	ssrc, ok := st.vc.UsersToSSRC[d.Args[0].Value.(*discordgo.User).ID]
	st.vc.Unlock()
	if !ok {
		return "This person needs to speak in the voice channel before i can set the volume (i need to know the ssrc)", nil
	}

	st.mixer.SetVolume(ssrc, float32(vol))

	return fmt.Sprintf("Set volume of %s to %.1f%%", d.Args[0].Value.(*discordgo.User).Username, vol*100), nil
}

func CmdListStations(d *dcmd.Data) (interface{}, error) {

	output := "Live stations: ```\n"

	ActiveLock.RLock()
	for _, station := range ActiveStations {
		meta := station.Meta()
		output += fmt.Sprintf("%20s: %3d listeners\n", meta.Name, len(meta.Listeners))
	}
	ActiveLock.RUnlock()

	output += "```"
	return output, nil
}

func FindUserVoiceChannel(guild *discordgo.Guild, userID string) string {
	for _, v := range guild.VoiceStates {
		log(v.SessionID)
		if v.UserID == userID {
			return v.ChannelID
		}
	}

	return ""
}
