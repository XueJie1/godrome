package main

type repeatMode int

const (
	repeatOff repeatMode = iota
	repeatAll
	repeatOne
)

func (m repeatMode) next() repeatMode {
	switch m {
	case repeatOff:
		return repeatAll
	case repeatAll:
		return repeatOne
	default:
		return repeatOff
	}
}
