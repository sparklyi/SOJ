package judge0

import (
	"SOJ/internal/entity"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/spf13/viper"
	"io"
	"net/http"
)

func Run(client *http.Client, req *entity.Run) {
	url := "http://" + viper.GetString("judge0.addr") + "/submissions"

	data, _ := json.Marshal(req)
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Println(string(body))

}
