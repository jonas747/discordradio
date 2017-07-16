package main

import (
	"flag"
	"fmt"
	"github.com/jonas747/dcmd"
	"github.com/jonas747/discordgo"
	llog "log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Variables used for command line parameters
var (
	Token string

	runningChannels = make([]chan *sync.WaitGroup, 0)
	runningLock     sync.Mutex
	DG              *discordgo.Session
)

func init() {

	// flag.StringVar(&GuildID, "g", "288075199415320578", "GuilID")
	// flag.StringVar(&ChannelID, "c", "288079314384191488", "ChannelID")
	// flag.StringVar(&Token, "t", "", "Account Token")
	flag.Parse()
}

func main() {

	llog.SetOutput(os.Stderr)

	// Create a new Discord session using the provided login information.
	// Use discordgo.New(Token) to just use a token for login.
	dg, err := discordgo.New(os.Getenv("DG_TOKEN"))
	if err != nil {
		log("error creating Discord session,", err)
		return
	}
	DG = dg

	// Open the websocket and begin listening.
	err = dg.Open()
	if err != nil {
		log("error opening connection,", err)
		return
	}

	log("Bot is now running.  Press CTRL-C to exit.")

	sys := dcmd.NewStandardSystem("!r")
	dg.AddHandler(sys.HandleMessageCreate)
	InitCommands(sys)

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc
	Stop()
}

func Stop() {
	fmt.Println("stopping")

	var wg sync.WaitGroup
	runningLock.Lock()
	for _, v := range runningChannels {
		wg.Add(1)
		v <- &wg
	}
	runningLock.Unlock()
	fmt.Println("Waiting")
	wg.Wait()
	DG.Close()

	fmt.Println("Done Waiting")
	time.Sleep(time.Second)

}

func log(s ...interface{}) {
	fmt.Fprintln(os.Stderr, s...)
}
