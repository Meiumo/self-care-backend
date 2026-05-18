package liveresponse

// ResponseType defines which Live Response card variant to show.
type ResponseType int

const (
	// ResponseInstant — multi-turn chat card, follow-up in 1–2 h.
	ResponseInstant ResponseType = iota
	// ResponseAdvice — single advice card (no text input), follow-up in 3–12 h.
	ResponseAdvice
	// ResponseNothing — event is only logged; no AI response is generated.
	ResponseNothing
)

func (rt ResponseType) String() string {
	switch rt {
	case ResponseInstant:
		return "instant"
	case ResponseAdvice:
		return "advice"
	default:
		return "nothing"
	}
}

// TriggerResult carries event metadata into the generation pipeline.
type TriggerResult struct {
	Tag           string
	Weight        int
	Response      ResponseType
	FollowupHours int
	Opener        string // first-turn prompt shown to user in chat UI
}

// tempByType returns the inference temperature for each card variant.
func tempByType(rt ResponseType) float64 {
	if rt == ResponseAdvice {
		return 0.3
	}
	return 0.5
}
