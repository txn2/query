// Package query implements an api for adding and executing Lucene queries
// associate with an account.
package query

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/pkg/errors"

	"github.com/Masterminds/sprig"
	"github.com/gin-gonic/gin"
	"github.com/txn2/ack"
	"github.com/txn2/es/v2"
	"github.com/txn2/micro"
	"github.com/txn2/tm"
	"go.uber.org/zap"
)

const IdxQuery = "queries"

// Config
type Config struct {
	Logger     *zap.Logger
	HttpClient *micro.Client

	// used for communication with Elasticsearch
	// if nil, HttpClient will be used.
	Elastic       *es.Client
	ElasticServer string

	// for storage and retrieval of system_ queries
	SystemIdxPrefix string
}

// Api
type Api struct {
	*Config
}

// NewApi
func NewApi(cfg *Config) (*Api, error) {
	a := &Api{Config: cfg}

	if a.Elastic == nil {
		// Configure an elastic client
		a.Elastic = es.CreateClient(es.Config{
			Log:           cfg.Logger,
			HttpClient:    cfg.HttpClient.Http,
			ElasticServer: cfg.ElasticServer,
		})
	}

	// check for elasticsearch a few times before failing
	// this reduces a reliance on restarts when a full system is
	// spinning up
	backOff := []int{10, 10, 15, 15, 30, 30, 45}
	for _, boff := range backOff {
		code, _, _ := a.Elastic.Get("")
		a.Logger.Info("Attempting to contact Elasticsearch", zap.String("server", a.Elastic.ElasticServer))

		if code == 200 {
			a.Logger.Info("Connection to Elastic search successful.", zap.String("server", a.Elastic.ElasticServer))
			break
		}

		a.Logger.Warn("Unable to contact Elasticsearch rolling back off.", zap.Int("wait_seconds", boff))
		<-time.After(time.Duration(boff) * time.Second)
	}

	// send template mappings for query index
	_, _, errorResponse, err := a.Elastic.SendEsMapping(GetQueryTemplateMapping())
	if errorResponse != nil {

	}
	if err != nil {
		return nil, err
	}

	return a, nil
}

// ExecuteQueryHandler
func (a *Api) RunQueryHandler(c *gin.Context) {
	ak := ack.Gin(c)

	// RunQueryHandler must be security screened in
	// upstream middleware to protect account access.
	account := c.Param("account")

	query := &Query{}
	err := ak.UnmarshalPostAbort(query)
	if err != nil {
		a.Logger.Error("Unmarshal failure.", zap.Error(err))
		return
	}

	code, queryExecuteResult, errorResponse, err := a.ExecuteQuery(account, *query, c)
	if err != nil {
		a.Logger.Error("EsError", zap.Error(err))
		ak.SetPayloadType("EsError")
		ak.SetPayload("Error communicating with database.")
		if errorResponse != nil {
			ak.SetPayload(errorResponse.Message)
		}
		ak.GinErrorAbort(500, "EsError", err.Error())
		return
	}

	if code >= 400 && code < 500 {
		ak.SetPayload("Index not found.")
		ak.GinErrorAbort(404, "IndexNotFound", "Index not found")
		return
	}

	ak.SetPayloadType("EsResult")
	ak.GinSend(queryExecuteResult)
}

// ExecuteQuery
func (a *Api) ExecuteQuery(account string, query Query, c *gin.Context) (int, *es.Obj, *es.ErrorResponse, error) {
	queryResults := &es.Obj{}

	// is there a template to process?
	//
	if query.QueryTemplate != "" {
		// populate parameter map
		params := make(map[string]interface{})
		for _, param := range query.Parameters {
			if qv, ok := c.GetQuery(param.MachineName); ok {
				params[param.MachineName] = qv
				continue
			}
			params[param.MachineName] = param.DefaultValue
		}

		// process template
		a.Logger.Debug("Process query template.")
		tmpl, err := template.New("query_template").Funcs(sprig.TxtFuncMap()).Parse(query.QueryTemplate)
		if err != nil {
			a.Logger.Error("Error processing query template.", zap.Error(err))
			return 500, nil, nil, err
		}

		var qb bytes.Buffer
		err = tmpl.Execute(&qb, params)
		if err != nil {
			a.Logger.Error("Error executing query template.", zap.Error(err))
			return 500, nil, nil, err
		}

		query.QueryJson = qb.String()
		a.Logger.Debug("Parsed template query", zap.String("query", query.QueryJson))

		query.Query = &es.Obj{}

		err = json.Unmarshal(qb.Bytes(), query.Query)
		if err != nil {
			a.Logger.Error("Error Marshaling query json into object.", zap.Error(err))
			return 500, nil, nil, err
		}

		// process index pattern as template
		//
		a.Logger.Debug("Process idx_pattern template.")
		tmpl, err = template.New("idx_pattern").Funcs(sprig.TxtFuncMap()).Parse(query.IdxPattern)
		if err != nil {
			a.Logger.Error("Error processing idx_pattern template.", zap.Error(err))
			return 500, nil, nil, err
		}

		var ipb bytes.Buffer
		err = tmpl.Execute(&ipb, params)
		if err != nil {
			a.Logger.Error("Error executing idx_pattern template.", zap.Error(err))
			return 500, nil, nil, err
		}

		query.IdxPattern = ipb.String()

	}

	path := fmt.Sprintf("%s-data-%s%s/_search", account, query.Model, query.IdxPattern)

	code, errorResponse, err := a.Elastic.PostObjUnmarshal(path, query.Query, queryResults)
	if err != nil {
		a.Logger.Error("EsError", zap.Error(err))
		return code, nil, errorResponse, err
	}

	a.Logger.Info("Search", zap.Int("code", code), zap.String("path", path))

	return code, queryResults, errorResponse, nil
}

// ExecuteQueryHandler
func (a *Api) ExecuteQueryHandlerF(system bool) gin.HandlerFunc {

	return func(c *gin.Context) {
		ak := ack.Gin(c)

		// ExecuteQueryHandler must be security screened in
		// upstream middleware to protect account access.
		account := c.Param("account")

		queryLocation := account

		if system {
			queryLocation = a.SystemIdxPrefix
		}

		id := c.Param("id")
		code, queryResult, err := a.GetQuery(queryLocation, id)
		if err != nil {
			a.Logger.Error("EsError", zap.Error(err))
			ak.SetPayloadType("EsError")
			ak.SetPayload("Error communicating with database.")
			//if queryResult != nil {
			//	ak.SetPayload("Error communicating with database.")
			//}
			ak.GinErrorAbort(500, "EsError", err.Error())
			return
		}

		if code >= 400 && code < 500 {
			ak.SetPayload("Query " + id + " not found.")
			ak.GinErrorAbort(404, "QueryNotFound", "Query not found")
			return
		}

		// only attempt if there is no template
		if queryResult.Source.QueryTemplate == "" {
			qObj := &es.Obj{}

			err = json.Unmarshal([]byte(queryResult.Source.QueryJson), qObj)
			if err != nil {
				ak.GinErrorAbort(500, "QueryUnmarshalError", err.Error())
				return
			}

			queryResult.Source.Query = qObj
		}

		code, queryExecuteResult, errorResponse, err := a.ExecuteQuery(account, queryResult.Source, c)
		if err != nil {
			a.Logger.Error("EsError", zap.Error(err))
			ak.SetPayloadType("EsError")
			ak.SetPayload("Error communicating with database.")
			if errorResponse != nil {
				ak.SetPayload(errorResponse.Message)
			}
			ak.GinErrorAbort(500, "EsError", err.Error())
			return
		}

		if code >= 400 && code < 500 {
			ak.SetPayload("Query execution failed")
			ak.GinErrorAbort(404, "QueryFailure", "Index not found")
			return
		}

		ak.SetPayloadType("EsResult")
		ak.GinSend(queryExecuteResult)
	}
}

// UpsertQuery
func (a *Api) UpsertQuery(account string, query *Query) (int, es.Result, *es.ErrorResponse, error) {
	a.Logger.Info("Upsert query record", zap.String("account", account), zap.String("machine_name", query.MachineName))

	locFmt := "%s-%s/_doc/%s"

	// CONVENTION: if the account ends in an underscore "_" then
	// it is a system model (SYSTEM_IdxModel)
	if strings.HasSuffix(account, "_") {
		locFmt = "%s%s/_doc/%s"
	}

	return a.Elastic.PutObj(fmt.Sprintf(locFmt, account, IdxQuery, query.MachineName), query)
}

// UpsertQueryHandler
func (a *Api) UpsertQueryHandler(c *gin.Context) {
	ak := ack.Gin(c)

	// UpsertQueryHandler must be security screened in
	// upstream middleware to protect account access.
	account := c.Param("account")

	query := &Query{}
	err := ak.UnmarshalPostAbort(query)
	if err != nil {
		a.Logger.Error("Upsert failure.", zap.Error(err))
		return
	}

	if query.Query != nil {
		// convert query to json
		queryJson, err := json.Marshal(query.Query)
		if err != nil {
			ak.GinErrorAbort(500, "QueryUnmashalError", err.Error())
			return
		}

		query.QueryJson = string(queryJson)
		query.Query = nil
	}

	// ensure lowercase machine name
	query.MachineName = strings.ToLower(query.MachineName)

	code, esResult, errorResponse, err := a.UpsertQuery(account, query)
	if err != nil {
		a.Logger.Error("Upsert failure.", zap.Error(err))
		ak.SetPayloadType("ErrorMessage")
		ak.SetPayload("there was a problem upserting the query")
		if errorResponse != nil {
			ak.SetPayload(errorResponse.Message)
		}
		ak.GinErrorAbort(500, "UpsertError", err.Error())
		return
	}

	if code < 200 || code >= 300 {
		a.Logger.Error("Es returned a non 200")
		ak.SetPayloadType("EsError")
		ak.SetPayload(esResult)
		ak.GinErrorAbort(500, "EsError", "Es returned a non 200")
		return
	}

	ak.SetPayloadType("EsResult")
	ak.GinSend(esResult)

}

// Result returned from Elastic
type Result struct {
	es.Result
	Source Query `json:"_source"`
}

// GetModel
func (a *Api) GetQuery(account string, id string) (int, *Result, error) {

	locFmt := "%s-%s/_doc/%s"

	// CONVENTION: if the account ends in an underscore "_" then
	// it is a system model (SYSTEM_IdxModel)
	if strings.HasSuffix(account, "_") {
		locFmt = "%s%s/_doc/%s"
	}

	code, ret, err := a.Elastic.Get(fmt.Sprintf(locFmt, account, IdxQuery, id))
	if err != nil {
		a.Logger.Error("EsGetError", zap.Error(err))
		return code, nil, err
	}

	if code != 200 {
		a.Logger.Error("EsGetNon200", zap.ByteString("returned", ret))
		return code, nil, errors.New(string(ret))
	}

	queryResult := &Result{}
	err = json.Unmarshal(ret, queryResult)
	if err != nil {
		return code, nil, err
	}

	return code, queryResult, nil
}

// GetQueryHandler
func (a *Api) GetQueryHandler(c *gin.Context) {
	ak := ack.Gin(c)

	// GetModelHandler must be security screened in
	// upstream middleware to protect account access.
	account := c.Param("account")
	id := c.Param("id")
	code, queryResult, err := a.GetQuery(account, id)
	if err != nil {
		a.Logger.Error("EsError", zap.Error(err))
		ak.SetPayloadType("EsError")
		ak.SetPayload("Error communicating with database.")
		ak.GinErrorAbort(500, "EsError", err.Error())
		return
	}

	if code >= 400 && code < 500 {
		ak.SetPayload("Query " + id + " not found.")
		ak.GinErrorAbort(404, "QueryNotFound", "Model not found")
		return
	}

	query := &es.Obj{}

	err = json.Unmarshal([]byte(queryResult.Source.QueryJson), query)
	if err != nil {
		ak.GinErrorAbort(500, "QueryUnmarshalError", err.Error())
		return
	}

	queryResult.Source.Query = query

	ak.SetPayloadType("QueryResult")
	ak.GinSend(queryResult)
}

// Query
type Query struct {
	// a lowercase under score delimited uniq id
	MachineName string `json:"machine_name" mapstructure:"machine_name"`

	// short human readable display name
	DisplayName string `json:"display_name" mapstructure:"display_name"`

	// a single sentence description
	BriefDescription string `json:"description_brief" mapstructure:"description_brief"`

	// full documentation in markdown
	Description string `json:"description" mapstructure:"description"`

	// named parsers
	Parsers []string `json:"parsers" mapstructure:"parsers"`

	// belongs to a class of queries
	QueryClass string `json:"query_class" mapstructure:"query_class"`

	// used for grouping queries
	Group string `json:"group" mapstructure:"group"`

	// used for grouping queries
	Model string `json:"model" mapstructure:"model"`

	// pattern default "-*" eg. "-someset", "-ts-2019*"
	IdxPattern string `json:"idx_pattern" mapstructure:"idx_pattern"`

	// query object
	Query *es.Obj `json:"query,omitempty" mapstructure:"query"`

	// used to describe input parameters
	Parameters []tm.Model `json:"parameters,omitempty" mapstructure:"parameters"`

	// query json
	QueryJson string `json:"query_json,omitempty" mapstructure:"query_json"`

	// if a query template is present it will take the place of
	// the query and query_json fields
	QueryTemplate string `json:"query_template,omitempty" mapstructure:"query_template"`

	// describes the query output
	ResultFields []tm.Model `json:"fields" mapstructure:"fields"`
}

// GetModelsTemplateMapping
func GetQueryTemplateMapping() es.IndexTemplate {
	var properties = es.Obj{
		"machine_name": es.Obj{
			"type": "text",
		},
		"display_name": es.Obj{
			"type": "text",
		},
		"description_brief": es.Obj{
			"type": "text",
		},
		"description": es.Obj{
			"type": "text",
		},
		"parsers": es.Obj{
			"type": "keyword",
		},
		"query_class": es.Obj{
			"type": "keyword",
		},
		"group": es.Obj{
			"type": "keyword",
		},
		"model": es.Obj{
			"type": "keyword",
		},
		"idx_pattern": es.Obj{
			"type": "text",
		},
		"query_json": es.Obj{
			"type": "text",
		},
		"query_template": es.Obj{
			"type": "text",
		},
		"parameters": es.Obj{
			"type": "nested",
		},
		"result_fields": es.Obj{
			"type": "nested",
		},
	}
	var tmpl = es.Obj{
		"index_patterns": []string{"*-" + IdxQuery, "*_" + IdxQuery},
		"settings": es.Obj{
			"index": es.Obj{
				"number_of_shards": 1,
			},
		},
		"mappings": es.Obj{
			"_doc": es.Obj{
				"_source": es.Obj{
					"enabled": true,
				},
				"properties": properties,
			},
		},
	}
	return es.IndexTemplate{
		Name:     IdxQuery,
		Template: tmpl,
	}
}
