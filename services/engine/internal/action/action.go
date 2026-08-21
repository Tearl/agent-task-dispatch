package action

type Reason struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Decision struct {
	Action  string   `json:"action"`
	Allowed bool     `json:"allowed"`
	Reasons []Reason `json:"reasons"`
}

type Response struct {
	ResourceType     string     `json:"resourceType"`
	ResourceID       string     `json:"resourceId"`
	AggregateVersion int64      `json:"aggregateVersion"`
	Actions          []Decision `json:"actions"`
}

func Decide(name string, reasons ...Reason) Decision {
	if reasons == nil {
		reasons = []Reason{}
	}
	return Decision{Action: name, Allowed: len(reasons) == 0, Reasons: reasons}
}

func Because(code, message string) Reason {
	return Reason{Code: code, Message: message}
}
