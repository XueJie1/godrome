package player

// displayState chooses what the transport shows. A cache wait sets pause as well,
// so buffering and an in-progress seek must outrank pause.
func displayState(paused, buffering, seeking bool) State {
	if buffering || seeking {
		return Buffering
	}
	if paused {
		return Paused
	}
	return Playing
}
