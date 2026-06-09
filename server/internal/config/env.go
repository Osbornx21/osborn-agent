package config

import "os"

type LookupEnv func(name string) (string, bool)

func OSLookupEnv(name string) (string, bool) {
	return os.LookupEnv(name)
}

func mapLookupEnv(values map[string]string) LookupEnv {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}
