package utils

import "github.com/speps/go-hashids/v2"

type Hash struct {
	secret string
	length int
}

func New(secret string, length int) *Hash {
	return &Hash{
		secret: secret,
		length: length,
	}
}

func (h *Hash) HashidsEncode(params []int) (string, error) {
	hd := hashids.NewData()
	hd.Salt = h.secret
	hd.MinLength = h.length

	hid, err := hashids.NewWithData(hd)
	if err != nil {
		return "", err
	}
	return hid.Encode(params)
}

func (h *Hash) HashidsDecode(hash string) ([]int, error) {
	hd := hashids.NewData()
	hd.Salt = h.secret
	hd.MinLength = h.length

	hid, err := hashids.NewWithData(hd)
	if err != nil {
		return nil, err
	}
	return hid.DecodeWithError(hash)
}
