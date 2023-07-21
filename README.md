# pusher-ws-go

[![Go Reference](https://pkg.go.dev/badge/github.com/goguardian/pusher-ws-go/v2.svg)](https://pkg.go.dev/github.com/goguardian/pusher-ws-go/v2) [![Go Report Card](https://goreportcard.com/badge/github.com/goguardian/pusher-ws-go/v2)](https://goreportcard.com/report/github.com/goguardian/pusher-ws-go/v2) [![Go](https://github.com/goguardian/pusher-ws-go/actions/workflows/go.yml/badge.svg)](https://github.com/goguardian/pusher-ws-go/actions/workflows/go.yml) [![Coverage Status](https://coveralls.io/repos/github/goguardian/pusher-ws-go/badge.svg?branch=main)](https://coveralls.io/github/goguardian/pusher-ws-go?branch=main)

This package implements a clinet for Pusher Channels WebSocket API.

## Installation

```sh
go get github.com/goguardian/pusher-ws-go/v2
```

## Features

* [ ] Connect to app
	* [ ] Custom cluster
	* [ ] Insecure connection
* [ ] Subscribe to channel
	* [ ] Auth for private and presence channels
	* [ ] Custom auth parameters
	* [ ] Custom auth headers
* [ ] Unsubscribe from channel
* [ ] Bind to events
	* [ ] Bind at app level
	* [ ] Bind at channel level
	* [ ] Bind global at app level
	* [ ] Bind global at channel level
* [ ] Unbind events
* [ ] Presence channel member data
* [ ] Cancel subscribing
* [ ] Handle pong timeout/reconnect
