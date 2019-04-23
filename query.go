/*
   Copyright 2019 txn2
   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at
       http://www.apache.org/licenses/LICENSE-2.0
   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/
package query

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/txn2/ack"
	"github.com/txn2/es"
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

	// send template mappings for query index
	_, _, err := a.Elastic.SendEsMapping(GetQueryTemplateMapping())
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

	code, queryExecuteResult, err := a.ExecuteQuery(account, *query)
	if err != nil {
		a.Logger.Error("EsError", zap.Error(err))
		ak.SetPayloadType("EsError")
		ak.SetPayload("Error communicating with database.")
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
func (a *Api) ExecuteQuery(account string, query Query) (int, *es.Obj, error) {
	queryResults := &es.Obj{}

	path := fmt.Sprintf("%s-data-%s%s/_search", account, query.Model, query.IdxPattern)

	code, err := a.Elastic.PostObjUnmarshal(path, query.Query, queryResults)
	if err != nil {
		a.Logger.Error("EsError", zap.Error(err))
		return code, nil, err
	}

	a.Logger.Info("Search", zap.Int("code", code), zap.String("path", path))

	return code, queryResults, nil
}

// ExecuteQueryHandler
func (a *Api) ExecuteQueryHandler(c *gin.Context) {
	ak := ack.Gin(c)

	// ExecuteQueryHandler must be security screened in
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
		ak.GinErrorAbort(404, "QueryNotFound", "Query not found")
		return
	}

	qObj := &es.Obj{}

	err = json.Unmarshal([]byte(queryResult.Source.QueryJson), qObj)
	if err != nil {
		ak.GinErrorAbort(500, "QueryUnmarshalError", err.Error())
		return
	}

	queryResult.Source.Query = qObj

	code, queryExecuteResult, err := a.ExecuteQuery(account, queryResult.Source)
	if err != nil {
		a.Logger.Error("EsError", zap.Error(err))
		ak.SetPayloadType("EsError")
		ak.SetPayload("Error communicating with database.")
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

// UpsertQuery
func (a *Api) UpsertQuery(account string, query *Query) (int, es.Result, error) {
	a.Logger.Info("Upsert query record", zap.String("account", account), zap.String("machine_name", query.MachineName))

	return a.Elastic.PutObj(fmt.Sprintf("%s-%s/_doc/%s", account, IdxQuery, query.MachineName), query)
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

	// convert query to json
	queryJson, err := json.Marshal(query.Query)
	if err != nil {
		ak.GinErrorAbort(500, "QueryUnmashalError", err.Error())
		return
	}

	query.QueryJson = string(queryJson)
	query.Query = nil

	// ensure lowercase machine name
	query.MachineName = strings.ToLower(query.MachineName)

	code, esResult, err := a.UpsertQuery(account, query)
	if err != nil {
		a.Logger.Error("Upsert failure.", zap.Error(err))
		ak.SetPayloadType("ErrorMessage")
		ak.SetPayload("there was a problem upserting the query")
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

	code, ret, err := a.Elastic.Get(fmt.Sprintf("%s-%s/_doc/%s", account, IdxQuery, id))
	if err != nil {
		a.Logger.Error("EsError", zap.Error(err))
		return code, nil, err
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

	// query json
	QueryJson string `json:"query_json,omitempty" mapstructure:"query_json"`

	// describes the query output
	ResultFields []tm.Model `json:"fields" mapstructure:"fields"`
}

// GetModelsTemplateMapping
func GetQueryTemplateMapping() es.IndexTemplate {
	properties := es.Obj{
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
		"result_fields": es.Obj{
			"type": "nested",
		},
	}

	template := es.Obj{
		"index_patterns": []string{"*-" + IdxQuery},
		"settings": es.Obj{
			"index": es.Obj{
				"number_of_shards": 2,
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
		Template: template,
	}
}
