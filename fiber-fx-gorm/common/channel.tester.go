package common

import "log"

type ChannelPayload struct {
	Id int
}

type ChannelHandle struct {
	Handle chan ChannelPayload
}

func InitChannelHandle() *ChannelHandle {
	log.Println("InitChannelHandle] ")

	return &ChannelHandle{
		Handle: make(chan ChannelPayload),
	}
}

func ChannelDataHandler(ch chan ChannelPayload) {
	for {
		select {
		case data := <-ch:
			println("ChannelDataHandler] data: ", data.Id)
		}
	}
}
