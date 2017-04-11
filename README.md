# pusher-ws-go

[![GoDoc](https://godoc.org/github.com/goguardian/pusher-ws-go?status.svg)](https://godoc.org/github.com/goguardian/pusher-ws-go)

This package implements a Pusher websocket client. It is based on the official [Pusher JavaScript client libary](https://github.com/pusher/pusher-js) as well as [go-pusher](https://github.com/toorop/go-pusher).

## Installation
	$ go get github.com/goguardian/pusher-ws-go

## Features

* [x] Connect to app
	* [x] Custom cluster
	* [x] Insecure connection
* [x] Subscribe to channel
	* [x] Auth for private and presence channels
	* [ ] Custom auth parameters
	* [ ] Custom auth headers
* [x] Unsubscribe from channel
* [x] Bind to events
	* [x] Bind at app level
	* [x] Bind at channel level
	* [ ] Bind global at app level
	* [ ] Bind global at channel level
* [x] Unbind events
* [ ] Presence channel member data
* [ ] Cancel subscribing
