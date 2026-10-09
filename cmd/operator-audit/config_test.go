package main

import "testing"

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestConfigServe(t *testing.T) {
	c, err := loadConfig("serve", env(map[string]string{"DATABASE_URL": "postgres://w@db/operator", "INTERNAL_TOKEN": "t"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":5005" || c.InternalPort != "8005" || c.InternalToken != "t" {
		t.Fatalf("%+v", c)
	}
}

func TestConfigMigrate(t *testing.T) {
	c, err := loadConfig("migrate", env(map[string]string{"DATABASE_URL": "postgres://o@db/operator", "WRITER_ROLE": "audit_writer"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.WriterRole != "audit_writer" {
		t.Fatalf("%+v", c)
	}
}

func TestConfigErrors(t *testing.T) {
	cases := map[string]struct {
		cmd string
		env map[string]string
	}{
		"no command":      {"", map[string]string{"DATABASE_URL": "x"}},
		"unknown command": {"run", map[string]string{"DATABASE_URL": "x"}},
		"serve no db":     {"serve", map[string]string{"INTERNAL_TOKEN": "t"}},
		"serve no token":  {"serve", map[string]string{"DATABASE_URL": "x"}},
		"serve bad port":  {"serve", map[string]string{"DATABASE_URL": "x", "INTERNAL_TOKEN": "t", "INTERNAL_PORT": "http"}},
		"migrate no role": {"migrate", map[string]string{"DATABASE_URL": "x"}},
		"migrate no db":   {"migrate", map[string]string{"WRITER_ROLE": "w"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := loadConfig(c.cmd, env(c.env)); err == nil {
				t.Fatal("want error")
			}
		})
	}
}
