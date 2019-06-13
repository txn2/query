package query

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/txn2/ack"
	"github.com/txn2/es/v2"
	"go.uber.org/zap"
)

// SearchResults
type SearchResults struct {
	es.SearchResults
	Hits struct {
		Total    int      `json:"total"`
		MaxScore float64  `json:"max_score"`
		Hits     []Result `json:"hits"`
	} `json:"hits"`
}

// SearchResultsAck
type SearchResultsAck struct {
	ack.Ack
	Payload SearchResults `json:"payload"`
}

// SearchQueries
func (a *Api) SearchQueries(account string, searchObj *es.Obj) (int, SearchResults, *es.ErrorResponse, error) {
	queryResults := &SearchResults{}

	code, errorResponse, err := a.Elastic.PostObjUnmarshal(fmt.Sprintf("%s-%s/_search", account, IdxQuery), searchObj, queryResults)
	if errorResponse != nil {
		a.Logger.Error("EsErrorResponse", zap.String("es_error_response", errorResponse.Message))
	}
	if err != nil {
		a.Logger.Error("EsError", zap.Error(err))
		return code, *queryResults, errorResponse, err
	}

	return code, *queryResults, nil, nil
}

// SearchQueryHandler
func (a *Api) SearchQueryHandler(c *gin.Context) {
	ak := ack.Gin(c)

	obj := &es.Obj{}
	err := ak.UnmarshalPostAbort(obj)
	if err != nil {
		a.Logger.Error("Search failure.", zap.Error(err))
		return
	}

	// SearchQueryHandler must be security screened in
	// upstream middleware to protect account access.
	account := c.Param("account")

	code, esResult, errorResponse, err := a.SearchQueries(account, obj)
	if err != nil {
		ak.SetPayloadType("EsError")
		ak.SetPayload("Error communicating with database.")
		if errorResponse != nil {
			ak.SetPayload(errorResponse.Message)
		}
		ak.GinErrorAbort(500, "EsError", err.Error())
		return
	}

	if code >= 400 && code < 500 {
		ak.SetPayload(esResult)
		ak.GinErrorAbort(500, "SearchError", "There was a problem searching")
		return
	}

	ak.SetPayloadType("QuerySearchResults")
	ak.GinSend(esResult)
}
