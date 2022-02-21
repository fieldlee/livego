package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"

	"github.com/bigsillybear/livego/av"
	"github.com/bigsillybear/livego/configure"
	"github.com/bigsillybear/livego/protocol/rtmp"
	"github.com/bigsillybear/livego/protocol/rtmp/rtmprelay"

	jwtmiddleware "github.com/auth0/go-jwt-middleware"
	"github.com/dgrijalva/jwt-go"
	log "github.com/sirupsen/logrus"
)

type Response struct {
	w      http.ResponseWriter
	Status int         `json:"status"`
	Data   interface{} `json:"data"`
}

func (r *Response) SendJson() (int, error) {
	resp, _ := json.Marshal(r)
	r.w.Header().Set("Content-Type", "application/json")
	r.w.WriteHeader(r.Status)
	return r.w.Write(resp)
}

type Operation struct {
	Method string `json:"method"`
	URL    string `json:"url"`
	Stop   bool   `json:"stop"`
}

type OperationChange struct {
	Method    string `json:"method"`
	SourceURL string `json:"source_url"`
	TargetURL string `json:"target_url"`
	Stop      bool   `json:"stop"`
}

type ClientInfo struct {
	url              string
	rtmpRemoteClient *rtmp.Client
	rtmpLocalClient  *rtmp.Client
}

type Server struct {
	handler  av.Handler
	session  map[string]*rtmprelay.RtmpRelay
	rtmpAddr string
}

func NewServer(h av.Handler, rtmpAddr string) *Server {
	return &Server{
		handler:  h,
		session:  make(map[string]*rtmprelay.RtmpRelay),
		rtmpAddr: rtmpAddr,
	}
}

func JWTMiddleware(next http.Handler) http.Handler {
	isJWT := len(configure.Config.GetString("jwt.secret")) > 0
	if !isJWT {
		return next
	}

	log.Info("Using JWT middleware")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var algorithm jwt.SigningMethod
		if len(configure.Config.GetString("jwt.algorithm")) > 0 {
			algorithm = jwt.GetSigningMethod(configure.Config.GetString("jwt.algorithm"))
		}

		if algorithm == nil {
			algorithm = jwt.SigningMethodHS256
		}

		jwtMiddleware := jwtmiddleware.New(jwtmiddleware.Options{
			Extractor: jwtmiddleware.FromFirst(jwtmiddleware.FromAuthHeader, jwtmiddleware.FromParameter("jwt")),
			ValidationKeyGetter: func(token *jwt.Token) (interface{}, error) {
				return []byte(configure.Config.GetString("jwt.secret")), nil
			},
			SigningMethod: algorithm,
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err string) {
				res := &Response{
					w:      w,
					Status: 403,
					Data:   err,
				}
				res.SendJson()
			},
		})

		jwtMiddleware.HandlerWithNext(w, r, next.ServeHTTP)
	})
}

func (s *Server) Serve(l net.Listener) error {
	mux := http.NewServeMux()

	mux.Handle("/statics/", http.StripPrefix("/statics/", http.FileServer(http.Dir("statics"))))

	mux.HandleFunc("/control/push", func(w http.ResponseWriter, r *http.Request) {
		s.handlePush(w, r)
	})
	mux.HandleFunc("/control/pull", func(w http.ResponseWriter, r *http.Request) {
		s.handlePull(w, r)
	})
	mux.HandleFunc("/control/get", func(w http.ResponseWriter, r *http.Request) {
		s.handleGet(w, r)
	})
	mux.HandleFunc("/control/reset", func(w http.ResponseWriter, r *http.Request) {
		s.handleReset(w, r)
	})
	mux.HandleFunc("/control/delete", func(w http.ResponseWriter, r *http.Request) {
		s.handleDelete(w, r)
	})
	mux.HandleFunc("/stat/livestat", func(w http.ResponseWriter, r *http.Request) {
		s.GetLiveStatics(w, r)
	})
	http.Serve(l, JWTMiddleware(mux))
	return nil
}

type stream struct {
	Key             string `json:"key"`
	Url             string `json:"url"`
	StreamId        uint32 `json:"stream_id"`
	VideoTotalBytes uint64 `json:"video_total_bytes"`
	VideoSpeed      uint64 `json:"video_speed"`
	AudioTotalBytes uint64 `json:"audio_total_bytes"`
	AudioSpeed      uint64 `json:"audio_speed"`
}

type streams struct {
	Publishers []stream `json:"publishers"`
	Players    []stream `json:"players"`
}

//http://127.0.0.1:8090/stat/livestat
func (server *Server) GetLiveStatics(w http.ResponseWriter, req *http.Request) {
	res := &Response{
		w:      w,
		Data:   nil,
		Status: 200,
	}

	defer res.SendJson()

	rtmpStream := server.handler.(*rtmp.RtmpStream)
	if rtmpStream == nil {
		res.Status = 500
		res.Data = "Get rtmp stream information error"
		return
	}

	msgs := new(streams)

	rtmpStream.GetStreams().Range(func(key, val interface{}) bool {
		if s, ok := val.(*rtmp.Stream); ok {
			if s.GetReader() != nil {
				switch s.GetReader().(type) {
				case *rtmp.VirReader:
					v := s.GetReader().(*rtmp.VirReader)
					msg := stream{key.(string), v.Info().URL, v.ReadBWInfo.StreamId, v.ReadBWInfo.VideoDatainBytes, v.ReadBWInfo.VideoSpeedInBytesperMS,
						v.ReadBWInfo.AudioDatainBytes, v.ReadBWInfo.AudioSpeedInBytesperMS}
					msgs.Publishers = append(msgs.Publishers, msg)
				}
			}
		}
		return true
	})

	rtmpStream.GetStreams().Range(func(key, val interface{}) bool {
		ws := val.(*rtmp.Stream).GetWs()
		ws.Range(func(k, v interface{}) bool {
			if pw, ok := v.(*rtmp.PackWriterCloser); ok {
				if pw.GetWriter() != nil {
					switch pw.GetWriter().(type) {
					case *rtmp.VirWriter:
						v := pw.GetWriter().(*rtmp.VirWriter)
						msg := stream{key.(string), v.Info().URL, v.WriteBWInfo.StreamId, v.WriteBWInfo.VideoDatainBytes, v.WriteBWInfo.VideoSpeedInBytesperMS,
							v.WriteBWInfo.AudioDatainBytes, v.WriteBWInfo.AudioSpeedInBytesperMS}
						msgs.Players = append(msgs.Players, msg)
					}
				}
			}
			return true
		})
		return true
	})

	//resp, _ := json.Marshal(msgs)
	res.Data = msgs
}

//http://127.0.0.1:8090/control/pull?&oper=start&app=live&channel_key=rfBd56ti2SMtYvSgD5xAV0YU99zampta7Z7S575KLkIZ9PYk&url=rtmp://{pull_streaming_server}/live/movie
func (s *Server) handlePull(w http.ResponseWriter, req *http.Request) {
	var retString string
	var err error

	res := &Response{
		w:      w,
		Data:   nil,
		Status: 200,
	}

	defer res.SendJson()

	if req.ParseForm() != nil {
		res.Status = 400
		res.Data = "url: /control/pull?&oper=start&app=live&channel_key=rfBd56ti2SMtYvSgD5xAV0YU99zampta7Z7S575KLkIZ9PYk&url=rtmp://{pull_streaming_server}/live/movie"
		return
	}

	oper := req.Form.Get("oper")
	app := req.Form.Get("app")
	channelKey := req.Form.Get("channel_key")
	url := req.Form.Get("url")

	log.Debugf("control pull: oper=%v, app=%v, channelKey=%v, url=%v", oper, app, channelKey, url)
	if (len(app) <= 0) || (len(channelKey) <= 0) || (len(url) <= 0) {
		res.Status = 400
		res.Data = "control pull parameter error, please check them."
		return
	}

	publishUrl := "rtmp://127.0.0.1" + s.rtmpAddr + "/" + app + "/" + channelKey
	playUrl := url

	keyString := "pull:" + app + "/" + channelKey
	if oper == "stop" {
		pullRtmprelay, found := s.session[keyString]

		if !found {
			retString = fmt.Sprintf("session key[%s] not exist, please check it again.", keyString)
			res.Status = 400
			res.Data = retString
			return
		}
		log.Debugf("rtmprelay stop pull %s from %s", publishUrl, playUrl)
		pullRtmprelay.Stop()

		delete(s.session, keyString)
		retString = fmt.Sprintf("<h1>pull url stop %s ok</h1></br>", url)
		res.Data = retString
		log.Debugf("pull stop return %s", retString)
	} else {
		pullRtmprelay := rtmprelay.NewRtmpRelay(&playUrl, &publishUrl)
		log.Debugf("rtmprelay start pull %s from %s", publishUrl, playUrl)
		err = pullRtmprelay.Start()
		if err != nil {
			retString = fmt.Sprintf("pull error=%v", err)
			res.Status = 400
		} else {
			s.session[keyString] = pullRtmprelay
			retString = fmt.Sprintf("<h1>pull url start %s ok</h1></br>", url)
		}

		res.Data = retString
		log.Debugf("pull start return %s", retString)
	}
}

//http://127.0.0.1:8090/control/push?&oper=start&app=live&channel_key=rfBd56ti2SMtYvSgD5xAV0YU99zampta7Z7S575KLkIZ9PYk&url=rtmp://{push_streaming_server}/live/movie
func (s *Server) handlePush(w http.ResponseWriter, req *http.Request) {
	var retString string
	var err error

	res := &Response{
		w:      w,
		Data:   nil,
		Status: 200,
	}

	defer res.SendJson()

	if req.ParseForm() != nil {
		res.Data = "url: /control/push?&oper=start&app=live&channel_key=rfBd56ti2SMtYvSgD5xAV0YU99zampta7Z7S575KLkIZ9PYk&url=rtmp://{push_streaming_server}/live/movie"
		return
	}

	oper := req.Form.Get("oper")
	app := req.Form.Get("app")
	channelKey := req.Form.Get("channel_key")
	url := req.Form.Get("url")

	log.Debugf("control push: oper=%v, app=%v, channelKey=%v, url=%v", oper, app, channelKey, url)
	if (len(app) <= 0) || (len(channelKey) <= 0) || (len(url) <= 0) {
		res.Status = 400
		res.Data = "control push parameter error, please check them."
		return
	}

	playUrl := "rtmp://127.0.0.1" + s.rtmpAddr + "/" + app + "/" + channelKey
	publishUrl := url

	keyString := "push:" + app + "/" + channelKey
	if oper == "stop" {
		pushRtmprelay, found := s.session[keyString]
		if !found {
			retString = fmt.Sprintf("<h1>session key[%s] not exist, please check it again.</h1>", keyString)
			res.Status = 400
			res.Data = retString
			return
		}
		log.Debugf("rtmprelay stop push %s from %s", publishUrl, playUrl)
		pushRtmprelay.Stop()

		delete(s.session, keyString)
		retString = fmt.Sprintf("<h1>push url stop %s ok</h1></br>", url)
		res.Data = retString
		log.Debugf("push stop return %s", retString)
	} else {
		pushRtmprelay := rtmprelay.NewRtmpRelay(&playUrl, &publishUrl)
		log.Debugf("rtmprelay start push %s from %s", publishUrl, playUrl)
		err = pushRtmprelay.Start()
		if err != nil {
			retString = fmt.Sprintf("push error=%v", err)
			res.Status = 400
		} else {
			retString = fmt.Sprintf("<h1>push url start %s ok</h1></br>", url)
			s.session[keyString] = pushRtmprelay
		}

		res.Data = retString
		log.Debugf("push start return %s", retString)
	}
}

//http://127.0.0.1:8090/control/reset?room={room}
func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	res := &Response{
		w:      w,
		Data:   nil,
		Status: 200,
	}
	defer res.SendJson()

	if err := r.ParseForm(); err != nil {
		res.Status = 400
		res.Data = "url: /control/reset?room={room}"
		return
	}
	room := r.Form.Get("room")

	if len(room) == 0 {
		res.Status = 400
		res.Data = "url: /control/reset?room={room}"
		return
	}

	msg, err := configure.RoomKeys.SetKey(room)

	if err != nil {
		msg = err.Error()
		res.Status = 400
	}

	res.Data = msg
}

//http://127.0.0.1:8090/control/get?room={room}
func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	res := &Response{
		w:      w,
		Data:   nil,
		Status: 200,
	}
	defer res.SendJson()

	if err := r.ParseForm(); err != nil {
		res.Status = 400
		res.Data = "url: /control/get?room={room}"
		return
	}

	room := r.Form.Get("room")

	if len(room) == 0 {
		res.Status = 400
		res.Data = "url: /control/get?room={room}"
		return
	}

	msg, err := configure.RoomKeys.GetKey(room)
	if err != nil {
		msg = err.Error()
		res.Status = 400
	}
	res.Data = msg
}

//http://127.0.0.1:8090/control/delete?room={room}
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	res := &Response{
		w:      w,
		Data:   nil,
		Status: 200,
	}
	defer res.SendJson()

	if err := r.ParseForm(); err != nil {
		res.Status = 400
		res.Data = "url: /control/delete?room={room}"
		return
	}

	room := r.Form.Get("room")

	if len(room) == 0 {
		res.Status = 400
		res.Data = "url: /control/delete?room={room}"
		return
	}

	if configure.RoomKeys.DeleteChannel(room) {
		res.Data = "Ok"
		return
	}
	res.Status = 404
	res.Data = "room not found"
}
