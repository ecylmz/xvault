package auth

func RedactedStatus(c Cookies) map[string]string {
	out := map[string]string{"auth_token": "missing", "ct0": "missing", "twid": "missing"}
	if c.AuthToken != "" {
		out["auth_token"] = "present"
	}
	if c.CT0 != "" {
		out["ct0"] = "present"
	}
	if c.TWID != "" {
		out["twid"] = "present"
	}
	return out
}
