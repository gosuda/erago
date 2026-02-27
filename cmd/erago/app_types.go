package main

import (
	"time"

	tea "charm.land/bubbletea/v2"
	eruntime "github.com/gosuda/erago/runtime"
)

type appConfig struct {
	base  string
	entry string
	savef string
}

type vmStartedMsg struct {
	events <-chan tea.Msg
}

type vmOutputMsg struct {
	out eruntime.Output
}

type vmDoneMsg struct {
	err error
}

type vmInputResp struct {
	value   string
	timeout bool
}

type vmPromptMsg struct {
	req  eruntime.InputRequest
	resp chan vmInputResp
}

type vmTimeoutMsg struct {
	seq int
}

type vmCountdownMsg struct {
	seq int
}

type vmPollMsg struct{}

type pendingInput struct {
	req      eruntime.InputRequest
	resp     chan vmInputResp
	seq      int
	isWait   bool
	deadline time.Time
}
