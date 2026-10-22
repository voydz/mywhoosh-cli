package auth

type Session struct {
	Version int `json:"version"`
	Token string `json:"access_token"`
	WhooshID string `json:"whoosh_id"`
	DeviceID string `json:"device_id"`
}

type Store struct { Dir string }

func (s Store) Load() (Session, error) { return Session{}, nil }
