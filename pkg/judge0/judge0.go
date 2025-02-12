package judge0

import (
	"SOJ/internal/entity"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

func Run(client *http.Client, req *entity.Run, url string) (*entity.JudgeResult, error) {

	data, _ := json.Marshal(req)
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, errors.New("测评机请求失败")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var t entity.JudgeResult
	_ = json.Unmarshal(body, &t)

	return &t, nil

}
