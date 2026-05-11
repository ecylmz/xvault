package auth

func FromEnvironment() Cookies {
	return fromMap(getenv)
}
