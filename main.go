package main

import (
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/fiorix/go-eventsocket/eventsocket"
)

var apiCommandNotOkay = errors.New("Body of api response did NOT start with +OK")

var uuidRexp = regexp.MustCompile("\\w{8}-\\w{4}-\\w{4}-\\w{4}-\\w{12}")

const audioFile = "ivr-you_lose.wav"
const callToAddress = "18775437013@172.16.19.89:52173"
const logAllEvents = false
const totalCalls = 10
const callsPerSecond = 1

type callState int

const (
	stateNew callState = iota
	statePlaying
	stateWaitingHangup
	stateDone
)

type inprogressCall struct {
	uuid  string
	state callState
}

func main() {
	c, err := eventsocket.Dial("localhost:8021", "ClueCon")

	if err != nil {
		log.Fatal(err)
		return
	}

	defer c.Close()

	// instruct freeswitch to send us ALL events
	c.Send("events json ALL")

	// start reading events
	eventChan := make(chan *eventsocket.Event, 32)
	go readEvents(c, eventChan)

	// setup a tick for keeping track of making new calls etc.
	tick := time.Tick(200 * time.Millisecond)

	// keep a map of calls
	calls := make(map[string]*inprogressCall)

	// start with one new call
	call, err := newCall(c)
	if err != nil {
		panic(err)
	}
	calls[call.uuid] = call

	callsCount := 1

	for {
		select {
		case ev := <-eventChan:
			uuid := ev.Get("Unique-Id")

			if len(uuid) == 0 {
				continue
			}

			call := calls[uuid]

			if call != nil {
				call.handleCallEvent(c, ev)
			}

			// clean up any done calls
			for _, call := range calls {
				if call.state == stateDone {
					delete(calls, call.uuid)
				}
			}
		case <-tick:
			if callsCount < totalCalls {
				call, err := newCall(c)
				if err != nil {
					panic(err)
				}
				calls[call.uuid] = call

				callsCount += 1
			}

			if len(calls) == 0 {
				fmt.Println("\n\nAll calls done!\n\n")
				return
			}
		}
	}
}

func grabUuidFromOKBody(ev *eventsocket.Event) (string, error) {
	if !strings.HasPrefix(ev.Body, "+OK") {
		fmt.Println("Body: ", ev.Body)
		return "", apiCommandNotOkay
	}

	return uuidRexp.FindString(ev.Body), nil
}

func readEvents(c *eventsocket.Connection, eventStream chan *eventsocket.Event) {
	for {
		ev, err := c.ReadEvent()
		if err != nil {
			log.Fatal(err)
			panic("Error!")
		}

		if logAllEvents {
			fmt.Println("\n")
			fmt.Println("Event!")
			ev.PrettyPrint()
			fmt.Println("\n")
		}

		eventStream <- ev
	}
}

func newCall(c *eventsocket.Connection) (*inprogressCall, error) {
	fmt.Println("New call")

	originateEv, err := c.Send("api originate sofia/external/sip:" + callToAddress + " &park")
	if err != nil {
		log.Fatal(err)
		return nil, err
	}

	uuid, err := grabUuidFromOKBody(originateEv)
	if err != nil {
		log.Fatal(err)
		return nil, err
	}

	fmt.Println("Call created, uuid: ", uuid)

	return &inprogressCall{uuid: uuid, state: stateNew}, nil
}

func (call *inprogressCall) handleCallEvent(c *eventsocket.Connection, ev *eventsocket.Event) {
	switch call.state {
	case stateNew:
		call.handleEventNew(c, ev)
	case statePlaying:
		call.handleEventPlaying(c, ev)
	case stateWaitingHangup:
		call.handleEventWaitingHangup(c, ev)
	case stateDone:
		fmt.Println("Ignoring event for call uuid: ", call.uuid, " since it is hung up/destroyed.")
	}
}

func (call *inprogressCall) handleEventNew(c *eventsocket.Connection, ev *eventsocket.Event) {
	if ev.Get("Answer-State") == "ringing" {
		fmt.Println("Ringing...")
	} else if ev.Get("Answer-State") == "answered" {
		fmt.Println("Answered!")
		call.playAudio(c, ev)
	} else {
		fmt.Println("Answer-State is '", ev.Get("Answer-State"), "', waiting for answered")
	}
}

func (call *inprogressCall) handleEventPlaying(c *eventsocket.Connection, ev *eventsocket.Event) {
	if ev.Get("Event-Name") == "PLAYBACK_START" {
		fmt.Println("PLAYBACK_START")
	} else if ev.Get("Event-Name") == "PLAYBACK_STOP" {
		fmt.Println("PLAYBACK_STOP")
		call.scheduleHangup(c, ev)
	} else {
		fmt.Println("Event: ", ev.Get("Event-Name"), "waiting for PLAYBACK_STOP")
	}
}

func (call *inprogressCall) handleEventWaitingHangup(c *eventsocket.Connection, ev *eventsocket.Event) {
	if ev.Get("Channel-State") == "CS_DESTROY" {
		fmt.Println("CS_DESTROY")
		call.state = stateDone
	} else {
		fmt.Println("Channel-State: ", ev.Get("Event-Name"), " waiting for CS_DESTROY")
	}
}

func (call *inprogressCall) playAudio(c *eventsocket.Connection, ev *eventsocket.Event) {
	// send play command
	playEv, err := c.Send(fmt.Sprintf("api uuid_broadcast %s %s aleg", call.uuid, audioFile))

	if logAllEvents {
		playEv.PrettyPrint()
	}

	if err != nil {
		panic(err)
	}

	_, err = grabUuidFromOKBody(playEv)
	if err != nil {
		panic(err)
	}

	call.state = statePlaying
}

func (call *inprogressCall) scheduleHangup(c *eventsocket.Connection, ev *eventsocket.Event) {
	// hang up, we're done!
	_, err := c.Send(fmt.Sprintf("api sched_hangup +1 %s", call.uuid))

	if err != nil {
		panic(err)
	}

	call.state = stateWaitingHangup
	fmt.Println("Scheduled hangup")
}
