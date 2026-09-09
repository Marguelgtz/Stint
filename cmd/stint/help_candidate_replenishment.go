package main

func init() {
	for i := range cmdStart.flags {
		if cmdStart.flags[i].name == "--network-candidate-attempts" {
			cmdStart.flags[i].purpose = "maximum rented Vast machines to test during startup/network qualification; stale offers are replaced without consuming an attempt"
			break
		}
	}
	cmdStart.notes = append(cmdStart.notes,
		"Marketplace offers that disappear before rental do not consume --network-candidate-attempts. Stint refreshes Vast, adds unseen replacement machines, and continues with the same paid-attempt number when possible.",
	)
}
