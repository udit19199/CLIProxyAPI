package config

// HomeConfig is a minimal stub for Home control plane settings.
type HomeConfig struct {
	Enabled bool          `yaml:"-" json:"-"`
	Host    string        `yaml:"-" json:"-"`
	Port    int           `yaml:"-" json:"-"`
	TLS     HomeTLSConfig `yaml:"-" json:"-"`
}

// HomeTLSConfig is a minimal stub for Home TLS settings.
type HomeTLSConfig struct {
	Enable              bool   `yaml:"-" json:"-"`
	ServerName          string `yaml:"-" json:"-"`
	CACert              string `yaml:"-" json:"-"`
	ClientCert          string `yaml:"-" json:"-"`
	ClientKey           string `yaml:"-" json:"-"`
	UseTargetServerName bool   `yaml:"-" json:"-"`
}
