package cmd

import (
	"os"

	"github.com/spf13/viper"
)

func initConfig() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	viper.SetConfigName(".moc")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(home)
	viper.SetDefault("region", "us-east-1")
	viper.SetDefault("profile", "")
	viper.ReadInConfig()
}
