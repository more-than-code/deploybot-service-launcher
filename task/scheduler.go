package task

import (
	"bytes"
	"container/list"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kelseyhightower/envconfig"
	"github.com/more-than-code/deploybot-service-api/api"
	"github.com/more-than-code/deploybot-service-api/model"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var gTicker *time.Ticker
var gEventQueue = list.New()

type SchedulerConfig struct {
	ApiBaseUrl     string `envconfig:"API_BASE_URL"`
	ApiAccessToken string `envconfig:"API_ACCESS_TOKEN"`
	PkUsername     string `envconfig:"PK_USERNAME"`
	PkPassword     string `envconfig:"PK_PASSWORD"`
}

type Scheduler struct {
	runner *Runner
	cfg    SchedulerConfig
}

func NewScheduler() *Scheduler {
	var cfg SchedulerConfig
	err := envconfig.Process("", &cfg)
	if err != nil {
		panic(err)
	}

	return &Scheduler{runner: NewRunner(), cfg: cfg}
}

func (s *Scheduler) PushEvent(e model.Event) {
	gEventQueue.PushBack(e)
}

func (s *Scheduler) PullEvent() model.Event {
	e := gEventQueue.Front()

	gEventQueue.Remove(e)

	return e.Value.(model.Event)
}

func (s *Scheduler) updateTaskStatus(pipelineId, taskId primitive.ObjectID, status string) {
	body, _ := json.Marshal(model.UpdateTaskStatusInput{
		PipelineId: pipelineId,
		TaskId:     taskId,
		Task:       struct{ Status string }{Status: status}})

	req, _ := http.NewRequest("PUT", s.cfg.ApiBaseUrl+"/taskStatus", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+s.cfg.ApiAccessToken)
	http.DefaultClient.Do(req)
}

func (s *Scheduler) ProcessPostTask(pipelineId, taskId primitive.ObjectID, status string) {
	body, _ := json.Marshal(model.UpdateTaskStatusInput{
		PipelineId: pipelineId,
		TaskId:     taskId,
		Task:       struct{ Status string }{Status: status}})

	req, _ := http.NewRequest("PUT", s.cfg.ApiBaseUrl+"/taskStatus", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+s.cfg.ApiAccessToken)
	http.DefaultClient.Do(req)
}

func (s *Scheduler) StreamWebhookHandler() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		body, _ := io.ReadAll(ctx.Request.Body)

		var sw model.StreamWebhook
		json.Unmarshal(body, &sw)

		log.Println(sw.Payload)

		req, _ := http.NewRequest("GET", fmt.Sprintf("%s/task?pid=%s&id=%s", s.cfg.ApiBaseUrl, sw.Payload.PipelineId.Hex(), sw.Payload.TaskId.Hex()), nil)
		req.Header.Set("Authorization", "Bearer "+s.cfg.ApiAccessToken)

		res, err := http.DefaultClient.Do(req)

		if err != nil {
			log.Println(err)
			ctx.JSON(http.StatusBadRequest, api.WebhookResponse{Msg: err.Error(), Code: api.CodeServerError})

			return
		}

		if res.StatusCode != 200 {
			log.Println(res.Body)
			ctx.JSON(http.StatusBadRequest, api.WebhookResponse{Msg: api.MsgClientError, Code: api.CodeClientError})

			return
		}

		body, _ = io.ReadAll(res.Body)

		var tRes api.GetTaskResponse
		json.Unmarshal(body, &tRes)

		task := tRes.Payload.Task

		var timer *time.Timer
		if task.Timeout > 0 {
			timer = s.cleanUp(time.Minute*time.Duration(task.Timeout), func() {
				s.updateTaskStatus(sw.Payload.PipelineId, task.Id, model.TaskTimedOut)
			})
		}

		go func() {
			s.updateTaskStatus(sw.Payload.PipelineId, task.Id, model.TaskInProgress)
			err := s.runner.DoTask(task, sw.Payload.Arguments)

			if timer != nil {
				timer.Stop()
			}

			if err != nil {
				log.Println(err)
				s.ProcessPostTask(sw.Payload.PipelineId, task.Id, model.TaskFailed)
			} else {
				s.ProcessPostTask(sw.Payload.PipelineId, task.Id, model.TaskDone)
			}
		}()

		ctx.JSON(http.StatusOK, api.WebhookResponse{})
	}
}

func (s *Scheduler) HealthCheckHandler() gin.HandlerFunc {
	return func(ctx *gin.Context) {

	}
}

func (s *Scheduler) cleanUp(delay time.Duration, job func()) *time.Timer {
	t := time.NewTimer(delay)
	go func() {
		for range t.C {
			job()
		}
	}()

	return t
}
