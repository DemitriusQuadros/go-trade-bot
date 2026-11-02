package settings

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-trade-bot/app/entities"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestValidation_MarketTypeAndPositionMode(t *testing.T) {
	// Valid market types: "", "spot", "futures"
	assert.True(t, (PlatformSettingsUpdateRequestDTO{MarketType: ""}).ValidMarketType())
	assert.True(t, (PlatformSettingsUpdateRequestDTO{MarketType: "spot"}).ValidMarketType())
	assert.True(t, (PlatformSettingsUpdateRequestDTO{MarketType: "futures"}).ValidMarketType())
	assert.False(t, (PlatformSettingsUpdateRequestDTO{MarketType: "options"}).ValidMarketType())
	assert.False(t, (PlatformSettingsUpdateRequestDTO{MarketType: "invalid"}).ValidMarketType())

	// Valid position modes: "", "one_way", "hedge"
	assert.True(t, (PlatformSettingsUpdateRequestDTO{PositionMode: ""}).ValidPositionMode())
	assert.True(t, (PlatformSettingsUpdateRequestDTO{PositionMode: "one_way"}).ValidPositionMode())
	assert.True(t, (PlatformSettingsUpdateRequestDTO{PositionMode: "hedge"}).ValidPositionMode())
	assert.False(t, (PlatformSettingsUpdateRequestDTO{PositionMode: "cross"}).ValidPositionMode())
	assert.False(t, (PlatformSettingsUpdateRequestDTO{PositionMode: "both"}).ValidPositionMode())
}

func TestMergeInto_MarketTypeAndPositionMode(t *testing.T) {
	existing := entities.Settings{
		ID:           1,
		MarketType:   "spot",
		PositionMode: "one_way",
	}

	// Non-empty values overwrite
	req := PlatformSettingsUpdateRequestDTO{
		MarketType:   "futures",
		PositionMode: "hedge",
	}
	merged := req.MergeInto(existing)
	assert.Equal(t, "futures", merged.MarketType)
	assert.Equal(t, "hedge", merged.PositionMode)

	// Omitted/empty values keep existing
	reqEmpty := PlatformSettingsUpdateRequestDTO{}
	mergedEmpty := reqEmpty.MergeInto(existing)
	assert.Equal(t, "spot", mergedEmpty.MarketType)
	assert.Equal(t, "one_way", mergedEmpty.PositionMode)
}

func TestToSettingsResponse_MarketTypeAndPositionMode(t *testing.T) {
	s := entities.Settings{
		MarketType:   "futures",
		PositionMode: "hedge",
	}
	resp := ToSettingsResponse(s)
	assert.Equal(t, "futures", resp.MarketType)
	assert.Equal(t, "hedge", resp.PositionMode)

	// Defaults when empty
	sDefault := entities.Settings{}
	respDefault := ToSettingsResponse(sDefault)
	assert.Equal(t, "spot", respDefault.MarketType)
	assert.Equal(t, "one_way", respDefault.PositionMode)
}

func TestPutSettings_InvalidMarketType_400(t *testing.T) {
	uc := new(mockUseCase)
	rec := httptest.NewRecorder()
	reqBody := []byte(`{"mode":"dryrun","market_type":"options"}`)
	NewSettingsHandler(uc).PutSettings(rec, httptest.NewRequest(http.MethodPut, "/settings", bytes.NewReader(reqBody)))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{"error":"invalid_market_type","message":"market_type must be one of spot, futures"}`, rec.Body.String())
	uc.AssertNotCalled(t, "Apply", mock.Anything, mock.Anything, mock.Anything)
}

func TestPutSettings_InvalidPositionMode_400(t *testing.T) {
	uc := new(mockUseCase)
	rec := httptest.NewRecorder()
	reqBody := []byte(`{"mode":"dryrun","position_mode":"isolated"}`)
	NewSettingsHandler(uc).PutSettings(rec, httptest.NewRequest(http.MethodPut, "/settings", bytes.NewReader(reqBody)))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{"error":"invalid_position_mode","message":"position_mode must be one of one_way, hedge"}`, rec.Body.String())
	uc.AssertNotCalled(t, "Apply", mock.Anything, mock.Anything, mock.Anything)
}

func TestPutSettings_ValidMarketTypeAndPositionMode(t *testing.T) {
	uc := new(mockUseCase)
	existing := entities.Settings{ID: 1, Mode: "dryrun", MarketType: "spot", PositionMode: "one_way"}
	uc.On("Get", mock.Anything).Return(existing, nil)

	applied := entities.Settings{ID: 1, Mode: "dryrun", MarketType: "futures", PositionMode: "hedge"}
	uc.On("Apply", mock.Anything, mock.MatchedBy(func(s entities.Settings) bool {
		return s.MarketType == "futures" && s.PositionMode == "hedge"
	}), false).Return(applied, nil)

	h := NewSettingsHandler(uc)
	body, _ := json.Marshal(PlatformSettingsUpdateRequestDTO{MarketType: "futures", PositionMode: "hedge"})
	req := httptest.NewRequest(http.MethodPut, "/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.PutSettings(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp PlatformSettingsUpdateResponseDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.True(t, resp.Applied)
	assert.Equal(t, "futures", resp.Settings.MarketType)
	assert.Equal(t, "hedge", resp.Settings.PositionMode)
}
