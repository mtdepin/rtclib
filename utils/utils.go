package utils

import (
	"encoding/json"
	"errors"
	"github.com/pion/ice/v2"
	"log"
	"strings"
)

func Marshal(m map[string]interface{}) string {
	if byt, err := json.Marshal(m); err != nil {
		log.Println(err.Error())
		return ""
	} else {
		return string(byt)
	}
}

func Unmarshal(str string) (map[string]interface{}, error) {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(str), &data); err != nil {
		log.Println(err.Error())
		return nil, err
	} else {
		return data, nil
	}
}

func FilterN2NAddress(address string) bool {
	if strings.HasPrefix(address, "192.168.100.") {
		log.Println("filter n2n address: %s", address)
		return true
	}

	return false
}

func ParseCandidateFromString(candidate string) (ice.Candidate, error) {
	candidateValue := strings.TrimPrefix(candidate, "candidate:")
	candi, err := ice.UnmarshalCandidate(candidateValue)
	if err != nil {
		if errors.Is(err, ice.ErrUnknownCandidateTyp) || errors.Is(err, ice.ErrDetermineNetworkType) {
			log.Println("parse candidate, Discarding remote candidate: %s", err)
			return nil, nil
		}
		return nil, err
	}

	return candi, nil
}
