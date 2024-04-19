package common

import "log"

type ChannelPayload struct {
	Id int
}

type ChannelHandle struct {
	Channel chan ChannelPayload
}

func InitChannelHandle() *ChannelHandle {
	log.Println("InitChannelHandle] ")

	return &ChannelHandle{
		Channel: make(chan ChannelPayload, 10),
	}
}

func ChannelDataHandler(handle *ChannelHandle) {
	go func() {
		for {
			select {
			case data := <-handle.Channel:
				println("ChannelDataHandler] data: ", data.Id)
			}
		}
	}()
}
