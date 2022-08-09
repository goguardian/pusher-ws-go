# pusher-ws-go

[![GoDoc](https://godoc.org/github.com/goguardian/pusher-ws-go?status.svg)](https://godoc.org/github.com/goguardian/pusher-ws-go)
[![Go Report Card](https://goreportcard.com/badge/github.com/goguardian/pusher-ws-go)](https://goreportcard.com/report/github.com/goguardian/pusher-ws-go)
[![Travis CI Build Status](https://travis-ci.org/goguardian/pusher-ws-go.svg?branch=master)](https://travis-ci.org/goguardian/pusher-ws-go)
[![Codecov](https://codecov.io/gh/goguardian/pusher-ws-go/branch/master/graph/badge.svg)](https://codecov.io/gh/goguardian/pusher-ws-go)

> NOTE: version 1 of this package is in maintanance mode. Use V2 instead (`github.com/goguardian/pusher-ws-go/v2`).

This package implements a Pusher websocket client. It is based on the official [Pusher JavaScript client libary](https://github.com/pusher/pusher-js) as well as [go-pusher](https://github.com/toorop/go-pusher).

## Installation

```sh
go get github.com/goguardian/pusher-ws-go
```

## Features

* [x] Connect to app
	* [x] Custom cluster
	* [x] Insecure connection
* [x] Subscribe to channel
	* [x] Auth for private and presence channels
	* [x] Custom auth parameters
	* [x] Custom auth headers
* [x] Unsubscribe from channel
* [x] Bind to events
	* [x] Bind at app level
	* [x] Bind at channel level
	* [ ] Bind global at app level
	* [ ] Bind global at channel level
* [x] Unbind events
* [x] Presence channel member data
* [ ] Cancel subscribing
* [ ] Handle pong timeout/reconnect

## Maintenance Mode

Version 1 branch has reached end of life and will only receive minor fixes in the future. Version 1 code is now hosted in the `v1` branch. `master` branch contains version 2 of the library. Please consider upgrading.
