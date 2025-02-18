package judge0

import (
	"SOJ/internal/constant"
	"SOJ/internal/entity"
	"bytes"
	"encoding/json"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"io"
	"net/http"
	"time"
)

type Judge struct {
	log      *zap.Logger
	client   *http.Client
	url      string
	judgeUrl string
}

func New(l *zap.Logger) (j *Judge) {
	j = new(Judge)
	j.log = l
	j.client = &http.Client{Timeout: 60 * time.Second}
	j.url = "http://" + viper.GetString("judge0.addr")
	j.judgeUrl = j.url + "/submissions/?wait=true"
	return
}

func (j *Judge) Run(req *entity.Run) *entity.JudgeResult {

	data, _ := json.Marshal(req)
	var t entity.JudgeResult
	//预设错误值, 函数不再处理error
	t.ID = constant.JudgeUnknown
	t.Description = constant.JudgeCode2Details[t.ID]
	t.Message = constant.JudgeCode2Details[t.ID]

	resp, err := j.client.Post(j.judgeUrl, "application/json", bytes.NewBuffer(data))
	if err != nil {
		j.log.Error("测评机请求失败", zap.Error(err))
		return &t
	}
	if resp.StatusCode != http.StatusCreated {
		return &t
	}
	//测评完成了 重置设置的消息(测评结果可能message为null不会读入到结构体)
	t.Message = ""
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(body, &t)

	return &t

}
