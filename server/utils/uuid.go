package utils

import "uuid"

func UUID() string {
	return uuid.New().String()
}
