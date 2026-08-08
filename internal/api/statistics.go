package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dude2k/MultiSpeed/internal/statistics"
)

const (
	maxStatisticsFilterValues      = 200
	maxStatisticsFilterValueLength = 128
)

func (s *Server) statistics(w http.ResponseWriter, r *http.Request) {
	query, err := s.parseStatisticsQuery(r)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_STATISTICS_QUERY", err.Error())
		return
	}
	report, err := s.statsService.Query(r.Context(), query)
	if err != nil {
		if errors.Is(err, statistics.ErrDimensionLimit) {
			writeError(w, r, http.StatusRequestEntityTooLarge, "STATISTICS_DIMENSION_LIMIT_EXCEEDED", fmt.Sprintf("The selected comparison dimension contains a value longer than %d bytes; choose another groupBy dimension or narrow the query to exclude that value.", statistics.MaxStatisticsDimensionBytes))
			return
		}
		if errors.Is(err, statistics.ErrOutputLimit) {
			writeError(w, r, http.StatusRequestEntityTooLarge, "STATISTICS_OUTPUT_LIMIT_EXCEEDED", "The query would produce more than 5,000 output points or 1,000 groups; use a coarser granularity, shorter range, or narrower filters.")
			return
		}
		if errors.Is(err, statistics.ErrResultLimit) {
			writeError(w, r, http.StatusRequestEntityTooLarge, "STATISTICS_LIMIT_EXCEEDED", "The query matches more than 50,000 results; narrow the range or filters.")
			return
		}
		s.logger.Error("statistics query failed", "request_id", requestIDFrom(r.Context()), "error", err)
		writeError(w, r, http.StatusInternalServerError, "STATISTICS_FAILED", "Statistics could not be calculated.")
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) parseStatisticsQuery(r *http.Request) (statistics.Query, error) {
	values := r.URL.Query()
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -30)
	to := now
	var err error
	if values.Get("from") != "" {
		from, err = time.Parse(time.RFC3339, values.Get("from"))
		if err != nil {
			return statistics.Query{}, errors.New("from must be RFC3339")
		}
	}
	if values.Get("to") != "" {
		to, err = time.Parse(time.RFC3339, values.Get("to"))
		if err != nil {
			return statistics.Query{}, errors.New("to must be RFC3339")
		}
	}
	if !to.After(from) {
		return statistics.Query{}, errors.New("to must be after from")
	}
	if to.Sub(from) > 24*time.Hour*3660 {
		return statistics.Query{}, errors.New("statistics range must not exceed ten years")
	}
	granularity := statistics.Granularity(values.Get("granularity"))
	if granularity == "" {
		granularity = statistics.GranularityDay
	}
	if granularity == statistics.GranularityRaw && to.Sub(from) > 90*24*time.Hour {
		return statistics.Query{}, errors.New("raw statistics are limited to a 90-day range")
	}
	switch granularity {
	case statistics.GranularityRaw, statistics.GranularityDay, statistics.GranularityISOWeek,
		statistics.GranularityMonth, statistics.GranularityYear, statistics.GranularityCustom:
	default:
		return statistics.Query{}, errors.New("granularity must be raw, day, iso-week, month, year, or custom")
	}
	timezone := values.Get("timezone")
	if timezone == "" {
		if settings, settingsErr := s.store.GetSettings(r.Context()); settingsErr == nil {
			timezone = settings.DefaultTimezone
		}
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return statistics.Query{}, errors.New("timezone must be a valid IANA timezone")
	}
	groupBy := statistics.Dimension(values.Get("groupBy"))
	switch groupBy {
	case statistics.DimensionNone, statistics.DimensionTask, statistics.DimensionInterface, statistics.DimensionSourceIP,
		statistics.DimensionProvider, statistics.DimensionServer, statistics.DimensionRouteProfile, statistics.DimensionPublicIP:
	default:
		return statistics.Query{}, errors.New("groupBy is unsupported")
	}
	taskIDs, err := statisticsListValues("taskId", values["taskId"])
	if err != nil {
		return statistics.Query{}, err
	}
	interfaceFilters, err := statisticsListValues("interface", values["interface"], values["interfaceName"])
	if err != nil {
		return statistics.Query{}, err
	}
	sourceIPs, err := statisticsListValues("sourceIp", values["sourceIp"])
	if err != nil {
		return statistics.Query{}, err
	}
	providerFilters, err := statisticsListValues("provider", values["provider"])
	if err != nil {
		return statistics.Query{}, err
	}
	serverIDs, err := statisticsListValues("serverId", values["serverId"])
	if err != nil {
		return statistics.Query{}, err
	}
	routeProfileIDs, err := statisticsListValues("routeProfileId", values["routeProfileId"])
	if err != nil {
		return statistics.Query{}, err
	}
	publicIPs, err := statisticsListValues("publicIp", values["publicIp"])
	if err != nil {
		return statistics.Query{}, err
	}
	query := statistics.Query{From: from, To: to, Granularity: granularity, ReportingTimezone: timezone, GroupBy: groupBy, Filter: statistics.Filter{
		TaskIDs: taskIDs, Interfaces: interfaceFilters, SourceIPs: sourceIPs, Providers: providerFilters,
		ServerIDs: serverIDs, RouteProfileIDs: routeProfileIDs, PublicIPs: publicIPs,
	}}
	return query, nil
}

func statisticsListValues(name string, valueSets ...[]string) ([]string, error) {
	var result []string
	for _, values := range valueSets {
		for _, value := range values {
			for {
				part, remainder, found := strings.Cut(value, ",")
				part = strings.TrimSpace(part)
				if part != "" {
					if len(part) > maxStatisticsFilterValueLength {
						return nil, fmt.Errorf("%s filter values must not exceed %d bytes", name, maxStatisticsFilterValueLength)
					}
					if len(result) >= maxStatisticsFilterValues {
						return nil, fmt.Errorf("%s filter may contain at most %d values", name, maxStatisticsFilterValues)
					}
					result = append(result, part)
				}
				if !found {
					break
				}
				value = remainder
			}
		}
	}
	return result, nil
}
