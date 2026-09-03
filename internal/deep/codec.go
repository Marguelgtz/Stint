package deep

import "encoding/json"

// marshalIndent / unmarshal are seams over encoding/json so tests can
// substitute scripted encoders if state encoding ever needs faking.
func marshalIndent(v any) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
