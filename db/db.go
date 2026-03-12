package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	//	"os"
	"time"

	"truckapi/internal/chrobinson"
	"truckapi/internal/httpdebug"
	"truckapi/pkg/config"

	"github.com/gofiber/websocket/v2"
	log "github.com/sirupsen/logrus"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var DB *gorm.DB
var PlatformDB *gorm.DB
var processedTrucks map[int64]bool

// Convert the time to UTC and then format it to YYYY-MM-DD
func formatToDateOnly(dateTimeStr string) (string, error) {
	parsedTime, err := time.Parse("2006-01-02 15:04:05", dateTimeStr)
	if err != nil {
		return "", err
	}
	return parsedTime.UTC().Format("2006-01-02"), nil
}

// InitializeDatabase initializes the SQLite database connection and performs auto migration for the tables.
func InitializeDatabase() {
	var err error
	dbPath := sqliteDBPath()
	DB, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		log.Fatalf("Error opening SQLite database: %v", err)
	}
	log.WithField("path", dbPath).Info("SQLite database connection established.")

	err = DB.AutoMigrate(&chrobinson.ShipmentInfo{}, &chrobinson.OfferResponse{}, &chrobinson.ShipmentDetailsRecord{}, &ChrobLoaderAudit{})
	if err != nil {
		log.Fatalf("Error migrating SQLite database: %v", err)
	}
	log.Info("SQLite database migration completed. Tables created/updated successfully.")
}

// InitializePlatformDatabase initializes the MySQL database connection.
func InitializePlatformDatabase() error {
	dsn := "dbassist:7EDQO6vKbUX2@tcp(10.0.30.125)/platform"

	var err error
	PlatformDB, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Errorf("Failed to open MySQL database: %v", err)
		return err
	}
	log.Info("MySQL database connection established.")

	sqlDB, err := PlatformDB.DB()
	if err != nil {
		log.Errorf("Failed to retrieve generic database object from MySQL connection: %v", err)
		return err
	}

	// Set connection pool settings
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(25)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)
	log.Info("MySQL database connection pool settings configured.")

	// Test the database connection
	if err = sqlDB.Ping(); err != nil {
		log.Errorf("Failed to ping MySQL database: %v", err)
		return err
	}

	log.Info("Successfully connected to the MySQL database.")
	return nil
}

// GetActiveTrucksAndLocations retrieves active trucks and their locations from the MySQL database.
func GetActiveTrucksAndLocations() ([]chrobinson.CombinedShipmentInfo, error) {
	var combinedInfos []chrobinson.CombinedShipmentInfo
	processedTruckIDs := make(map[int64]bool)

	// Get the current date in America/New_York (so DATE(updated_at) matches NY-based TIMESTAMPs)
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		// if for some reason loading NY fails, fall back to local
		loc = time.Local
	}
	nyToday := time.Now().In(loc).Format("2006-01-02")
	log.Infof("Querying pseudo_locations for updates on (NY date): %s", nyToday)

	rows, err := PlatformDB.Raw(`
		SELECT 
			p.id, 
			p.address, 
			p.from, 
			p.to, 
			p.truck_id, 
			p.created_at, 
			p.updated_at, 
			p.lat, 
			p.lng,
			t.id AS truck_id, 
			t.company_id, 
			t.truck_type_id, 
			t.length, 
			t.weight, 
			t.radius, 
			t.distance_min, 
			t.distance_max,
			u.name AS user_name,
			d.fullname AS driver_name,
			c.telegram AS telegram,
			c.phone_number AS phone_number,
			tt.name AS truck_type,
			du.name AS dispatcher_name
		FROM 
			pseudo_locations p
		JOIN (
			SELECT truck_id, MAX(updated_at) as max_updated_at
			FROM pseudo_locations
			WHERE DATE(updated_at) = ?
			GROUP BY truck_id
		) as latest_updates 
		ON p.truck_id = latest_updates.truck_id 
		AND p.updated_at = latest_updates.max_updated_at
		JOIN trucks t ON p.truck_id = t.id
		JOIN users u ON t.company_id = u.company_id
		JOIN truck_driver td ON t.id = td.truck_id
		JOIN drivers d ON td.driver_id = d.id
		JOIN companies c ON t.company_id = c.id
		JOIN truck_types tt ON t.truck_type_id = tt.id
		JOIN dispatchers_pivot dp ON u.id = dp.user_id
		JOIN users du ON dp.dispatcher_id = du.id
		ORDER BY p.truck_id, p.updated_at
		`, nyToday).Rows()
	if err != nil {
		log.Errorf("Error querying pseudo_locations with NY date %s: %v", nyToday, err)
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var combinedInfo chrobinson.CombinedShipmentInfo
		var telegram sql.NullString
		var phoneNumber sql.NullString
		var distanceMin sql.NullInt64
		var distanceMax sql.NullInt64
		var truckType sql.NullString
		var dispatcherName sql.NullString

		err := rows.Scan(
			&combinedInfo.LocationData.Id, &combinedInfo.LocationData.Address, &combinedInfo.LocationData.From, &combinedInfo.LocationData.To, &combinedInfo.LocationData.TruckId, &combinedInfo.LocationData.CreatedAt, &combinedInfo.LocationData.UpdatedAt, &combinedInfo.LocationData.Lat, &combinedInfo.LocationData.Lng,
			&combinedInfo.TruckData.Id, &combinedInfo.TruckData.CompanyId, &combinedInfo.TruckData.TruckTypeId, &combinedInfo.TruckData.Length, &combinedInfo.TruckData.Weight, &combinedInfo.TruckData.Radius, &distanceMin, &distanceMax,
			&combinedInfo.AdditionalData.UserName,
			&combinedInfo.AdditionalData.DriverName,
			&telegram,
			&phoneNumber,
			&truckType,
			&dispatcherName,
		)
		if err != nil {
			log.Errorf("Error scanning row: %v", err)
			return nil, err
		}

		combinedInfo.AdditionalData.TelegramLink = telegram.String
		combinedInfo.AdditionalData.PhoneNumber = phoneNumber.String
		combinedInfo.AdditionalData.TruckType = truckType.String
		combinedInfo.AdditionalData.DispatcherName = dispatcherName.String

		if distanceMin.Valid {
			combinedInfo.TruckData.DistanceMin = int(distanceMin.Int64)
		}
		if distanceMax.Valid {
			combinedInfo.TruckData.DistanceMax = int(distanceMax.Int64)
		}

		if !processedTruckIDs[combinedInfo.TruckData.Id] {
			combinedInfos = append(combinedInfos, combinedInfo)
			processedTruckIDs[combinedInfo.TruckData.Id] = true
		}
	}

	if len(combinedInfos) == 0 {
		log.Infof("No pseudo_locations updated in the specified dates.")
		return nil, nil
	}

	log.Infof("Found %d pseudo_locations updated in the specified dates.", len(combinedInfos))
	return combinedInfos, nil
}

func SearchAvailableShipmentsForTruck(apiClient *chrobinson.APIClient, combinedInfo chrobinson.CombinedShipmentInfo) ([]chrobinson.CombinedShipmentInfo, error) {
	//weightInPounds := float64(combinedInfo.TruckData.Weight)

	// Take raw strings but slice to YYYY-MM-DD
	fromDate := combinedInfo.LocationData.From[:10] // "2025-05-20"
	fromDateParsed, err := time.Parse("2006-01-02", fromDate)
	if err != nil {
		log.WithError(err).Error("Invalid fromDate format")
		return nil, err
	}
	toDate := fromDateParsed.AddDate(0, 0, 10).Format("2006-01-02")

	// ── clamp fromDate so it can’t be before today ─────────────────────
	today := time.Now().Truncate(24 * time.Hour) // midnight today
	if parsed, err := time.Parse("2006-01-02", fromDate); err == nil {
		if parsed.Before(today) {
			fromDate = today.Format("2006-01-02")
		}
	}

	distanceMax := combinedInfo.TruckData.DistanceMax
	if distanceMax == 0 {
		distanceMax = 1000
	}

	searchData := chrobinson.AvailableShipmentSearchRequest{
		PageIndex:  0,
		PageSize:   100,
		RegionCode: "NA",
		Modes:      []string{"F", "L", "R", "V", "H"},
		OriginRadiusSearch: &chrobinson.RadiusSearch{
			Coordinate: chrobinson.Coordinate{
				Lat: combinedInfo.LocationData.Lat,
				Lon: combinedInfo.LocationData.Lng,
			},
			Radius: chrobinson.Radius{
				Value:         combinedInfo.TruckData.Radius,
				UnitOfMeasure: "Standard",
			},
		},
		LoadDistanceRange: &chrobinson.Range{
			UnitOfMeasure: "Standard",
			Min:           0,
			Max:           7000,
		},
		LoadWeightRange: &chrobinson.Range{
			UnitOfMeasure: "Standard",
			Min:           0,
			Max:           10000,
			//Max: weightInPounds,
		},
		EquipmentLengthRange: &chrobinson.Range{
			UnitOfMeasure: "Standard",
			Min:           0,
			Max:           26,
		},
		AvailableForPickUpByDateRange: &chrobinson.DateRange{
			Min: fromDate,
			Max: toDate,
		},
		SortCriteria: &chrobinson.SortCriteria{
			Field:     "LoadNumber",
			Direction: "ascending",
		},
	}

	log.Infof("Sending available shipment search request for truck ID %d with search data: %+v", combinedInfo.TruckData.Id, searchData)
	response, err := apiClient.SearchAvailableShipments(searchData)
	if err != nil {
		log.Errorf("Error searching available shipments for truck ID %d: %v", combinedInfo.TruckData.Id, err)
		return nil, err
	}
	log.Infof("Received %d available shipments for truck ID %d", len(response.Results), combinedInfo.TruckData.Id)

	var combinedShipments []chrobinson.CombinedShipmentInfo
	for _, shipment := range response.Results {
		// if shipment.Origin.State == "" || shipment.Origin.Zip == "" {
		// 	state, zip, err := getCityDetails(shipment.Origin.City, shipment.Destination.City)
		// 	if err == nil {
		// 		shipment.Origin.State = state
		// 		shipment.Origin.Zip = zip
		// 	}
		// }
		// if shipment.Destination.State == "" || shipment.Destination.Zip == "" {
		// 	state, zip, err := getCityDetails(shipment.Destination.City, shipment.Origin.City)
		// 	if err == nil {
		// 		shipment.Destination.State = state
		// 		shipment.Destination.Zip = zip
		// 	}
		// }
		// ── QUICK FALLBACK: never call getCityDetails ────────────────
		if shipment.Origin.State == "" {
			shipment.Origin.State = "N/A"
		}
		if shipment.Origin.Zip == "" {
			shipment.Origin.Zip = "N/A"
		}
		if shipment.Destination.State == "" {
			shipment.Destination.State = "N/A"
		}
		if shipment.Destination.Zip == "" {
			shipment.Destination.Zip = "N/A"
		}

		combinedShipment := chrobinson.CombinedShipmentInfo{
			ShipmentInfo:              shipment,
			TruckData:                 combinedInfo.TruckData,
			LocationData:              combinedInfo.LocationData,
			AdditionalData:            combinedInfo.AdditionalData,
			BookingContactPhoneNumber: extractBookingPhone(shipment),
		}

		// var existingShipment chrobinson.ShipmentInfo
		combinedShipments = append(combinedShipments, combinedShipment)
	}

	combinedInfoJSON, _ := json.MarshalIndent(combinedInfo, "", "  ")
	log.Infof("Combined Shipment Info: %s", combinedInfoJSON)

	log.Infof("Total combined shipments for truck ID %d: %d", combinedInfo.TruckData.Id, len(combinedShipments))

	return combinedShipments, nil
}

func extractBookingPhone(shipment chrobinson.ShipmentInfo) string {
	// Prefer the explicit booking contact phone Robinson puts on the root object.
	if shipment.BookingContactPhoneNumber != "" {
		return shipment.BookingContactPhoneNumber
	}
	// Fallback: scan contact methods for a phone entry.
	for _, m := range shipment.Contact.ContactMethods {
		if strings.EqualFold(m.Method, "Phone") && m.Value != "" {
			return m.Value
		}
	}
	return ""
}
func GetActiveTrucksAndLocationsTruckStop() ([]chrobinson.CombinedShipmentInfo, error) {
	var combinedInfos []chrobinson.CombinedShipmentInfo
	processedTruckIDs := make(map[int64]bool)

	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		loc = time.Local
	}
	nyToday := time.Now().In(loc).Format("2006-01-02")
	log.Infof("Querying pseudo_locations for updates on (NY date): %s", nyToday)

	// Step 1: Get trucks for specific driver names
	namedRows, err := PlatformDB.Raw(`
		SELECT p.id, p.address, p.from, p.to, p.truck_id, p.created_at, p.updated_at, p.lat, p.lng,
		       t.id AS truck_id, t.company_id, t.truck_type_id, t.length, t.weight, t.radius, t.distance_min, t.distance_max,
		       u.name AS user_name, d.fullname AS driver_name, c.telegram, c.phone_number, tt.name AS truck_type, du.name AS dispatcher_name
		FROM pseudo_locations p
		JOIN (
			SELECT truck_id, MAX(updated_at) as max_updated_at
			FROM pseudo_locations
			WHERE DATE(updated_at) = ?
			GROUP BY truck_id
		) as latest_updates 
		  ON p.truck_id = latest_updates.truck_id AND p.updated_at = latest_updates.max_updated_at
		JOIN trucks t ON p.truck_id = t.id
		JOIN users u ON t.company_id = u.company_id
		JOIN truck_driver td ON t.id = td.truck_id
		JOIN drivers d ON td.driver_id = d.id
		JOIN companies c ON t.company_id = c.id
		JOIN truck_types tt ON t.truck_type_id = tt.id
		JOIN dispatchers_pivot dp ON u.id = dp.user_id
		JOIN users du ON dp.dispatcher_id = du.id
		WHERE LOWER(d.fullname) IN ('koka abuladze','gocha kupatadze','roman meparishvili','akaki lagvilava')
		ORDER BY p.truck_id, p.updated_at
	`, nyToday).Rows()
	if err != nil {
		return nil, err
	}
	defer namedRows.Close()

	for namedRows.Next() {
		combinedInfo, truckID, err := scanCombinedInfoRow(namedRows)
		if err != nil {
			return nil, err
		}
		combinedInfos = append(combinedInfos, combinedInfo)
		processedTruckIDs[truckID] = true
	}

	// Step 2: Get 10 additional random trucks not already included
	randomRows, err := PlatformDB.Raw(`
		SELECT p.id, p.address, p.from, p.to, p.truck_id, p.created_at, p.updated_at, p.lat, p.lng,
		       t.id AS truck_id, t.company_id, t.truck_type_id, t.length, t.weight, t.radius, t.distance_min, t.distance_max,
		       u.name AS user_name, d.fullname AS driver_name, c.telegram, c.phone_number, tt.name AS truck_type, du.name AS dispatcher_name
		FROM pseudo_locations p
		JOIN (
			SELECT truck_id, MAX(updated_at) as max_updated_at
			FROM pseudo_locations
			WHERE DATE(updated_at) = ?
			GROUP BY truck_id
		) as latest_updates 
		  ON p.truck_id = latest_updates.truck_id AND p.updated_at = latest_updates.max_updated_at
		JOIN trucks t ON p.truck_id = t.id
		JOIN users u ON t.company_id = u.company_id
		JOIN truck_driver td ON t.id = td.truck_id
		JOIN drivers d ON td.driver_id = d.id
		JOIN companies c ON t.company_id = c.id
		JOIN truck_types tt ON t.truck_type_id = tt.id
		JOIN dispatchers_pivot dp ON u.id = dp.user_id
		JOIN users du ON dp.dispatcher_id = du.id
		WHERE DATE(p.updated_at) = ? AND p.truck_id NOT IN ?
		ORDER BY RAND() LIMIT 10
	`, nyToday, nyToday, getTruckIDSlice(processedTruckIDs)).Rows()
	if err != nil {
		return nil, err
	}
	defer randomRows.Close()

	for randomRows.Next() {
		combinedInfo, truckID, err := scanCombinedInfoRow(randomRows)
		if err != nil {
			return nil, err
		}
		combinedInfos = append(combinedInfos, combinedInfo)
		processedTruckIDs[truckID] = true
	}

	return combinedInfos, nil
}

func getTruckIDSlice(idMap map[int64]bool) []int64 {
	ids := make([]int64, 0, len(idMap))
	for id := range idMap {
		ids = append(ids, id)
	}
	return ids
}

func scanCombinedInfoRow(rows *sql.Rows) (chrobinson.CombinedShipmentInfo, int64, error) {
	var combinedInfo chrobinson.CombinedShipmentInfo
	var telegram sql.NullString
	var phoneNumber sql.NullString
	var distanceMin sql.NullInt64
	var distanceMax sql.NullInt64
	var truckType sql.NullString
	var dispatcherName sql.NullString

	err := rows.Scan(
		&combinedInfo.LocationData.Id, &combinedInfo.LocationData.Address, &combinedInfo.LocationData.From, &combinedInfo.LocationData.To, &combinedInfo.LocationData.TruckId,
		&combinedInfo.LocationData.CreatedAt, &combinedInfo.LocationData.UpdatedAt, &combinedInfo.LocationData.Lat, &combinedInfo.LocationData.Lng,
		&combinedInfo.TruckData.Id, &combinedInfo.TruckData.CompanyId, &combinedInfo.TruckData.TruckTypeId, &combinedInfo.TruckData.Length, &combinedInfo.TruckData.Weight, &combinedInfo.TruckData.Radius, &distanceMin, &distanceMax,
		&combinedInfo.AdditionalData.UserName, &combinedInfo.AdditionalData.DriverName, &telegram, &phoneNumber, &truckType, &dispatcherName,
	)
	if err != nil {
		return combinedInfo, 0, err
	}

	combinedInfo.AdditionalData.TelegramLink = telegram.String
	combinedInfo.AdditionalData.PhoneNumber = phoneNumber.String
	combinedInfo.AdditionalData.TruckType = truckType.String
	combinedInfo.AdditionalData.DispatcherName = dispatcherName.String

	if distanceMin.Valid {
		combinedInfo.TruckData.DistanceMin = int(distanceMin.Int64)
	}
	if distanceMax.Valid {
		combinedInfo.TruckData.DistanceMax = int(distanceMax.Int64)
	}

	return combinedInfo, combinedInfo.TruckData.Id, nil
}

func updateShipmentInfo(shipment chrobinson.ShipmentInfo) chrobinson.ShipmentInfo {
	if shipment.Destination.State == "" || shipment.Destination.Zip == "" {
		state, zip, err := getCityDetails(shipment.Destination.City, shipment.Origin.City)
		if err == nil {
			shipment.Destination.State = state
			shipment.Destination.Zip = zip
		}
	}
	return shipment
}

func HandleWebSocketConnection(apiClient *chrobinson.APIClient, conn *websocket.Conn) {
	type wsMsg struct {
		messageType int
		data        []byte
	}

	writeCh := make(chan wsMsg, 200) // buffered
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // stop all helpers on exit

	// single writer goroutine
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case m, ok := <-writeCh:
				if !ok {
					return
				}
				if err := conn.WriteMessage(m.messageType, m.data); err != nil {
					log.WithError(err).Error("❌ WebSocket write failed — closing connection")
					conn.Close()
					cancel() // signal others to stop
					return
				}
			}
		}
	}()

	for {
		combinedInfos, err := GetActiveTrucksAndLocations()
		if err != nil {
			log.WithError(err).Error("Failed to get active trucks and locations")
			return
		}
		if len(combinedInfos) == 0 {
			log.Info("No active trucks or locations found for today.")
			return
		}

		totalShipmentsFound := 0
		today := time.Now().Truncate(24 * time.Hour)

		for _, combinedInfo := range combinedInfos {
			log.Infof("Processing truck ID %d", combinedInfo.TruckData.Id)

			shipments, err := SearchAvailableShipmentsForTruck(apiClient, combinedInfo)
			if err != nil {
				log.WithError(err).
					Errorf("Failed to search for shipments for truck ID %d", combinedInfo.TruckData.Id)
				continue
			}
			totalShipmentsFound += len(shipments)

			// Deduplicate shipments by LoadNumber, keeping the one with the latest UpdatedDateTime
			loadMap := make(map[string]chrobinson.CombinedShipmentInfo)
			for _, shipment := range shipments {
				loadNum := strconv.Itoa(shipment.ShipmentInfo.LoadNumber)
				existing, exists := loadMap[loadNum]
				if !exists {
					loadMap[loadNum] = shipment
				} else {
					// Compare UpdatedDateTime as RFC3339 time
					t1, err1 := time.Parse(time.RFC3339, shipment.ShipmentInfo.UpdatedDateTime)
					t2, err2 := time.Parse(time.RFC3339, existing.ShipmentInfo.UpdatedDateTime)
					if err1 == nil && err2 == nil {
						if t1.After(t2) {
							loadMap[loadNum] = shipment
						}
					}
				}
			}
			for _, shipment := range loadMap {
				// skip anything older than today
				if t, err := time.Parse(time.RFC3339, shipment.ReadyBy); err == nil {
					if t.Before(today) {
						continue
					}
				}

				data, err := json.Marshal(shipment)
				if err != nil {
					log.WithError(err).Error("Failed to marshal shipment data to JSON")
					continue
				}

				select {
				case writeCh <- wsMsg{messageType: websocket.TextMessage, data: data}:
				case <-ctx.Done():
					return
				}
			}
		}

		// send end-of-batch
		endMsg := map[string]string{"type": "end-of-batch"}
		endData, _ := json.Marshal(endMsg)

		select {
		case writeCh <- wsMsg{messageType: websocket.TextMessage, data: endData}:
		case <-ctx.Done():
			return
		}

		log.Infof("Query finished — trucks: %d, shipments: %d; sleeping 3m",
			len(combinedInfos), totalShipmentsFound)
		time.Sleep(3 * time.Minute)
	}
}

// FetchLoaderLocations calls the loader API and returns locations for a given source (e.g. TRUCKSTOP, CHROB).
func FetchLoaderLocations(source string) ([]chrobinson.LoaderLocation, error) {
	baseURL := strings.TrimRight(config.GetEnv(config.LoaderAPIBaseURL, "https://core.hfield.net"), "/")
	url := fmt.Sprintf("%s/api/v1/loader/locations?source=%s", baseURL, source)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	// Add required headers
	req.Header.Add("X-API-KEY", config.GetEnv(config.LoaderAPIKey, ""))

	client := &http.Client{
		Timeout:   15 * time.Second,
		Transport: httpdebug.NewTransport(http.DefaultTransport),
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("loader API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("loader API returned %d: %s", resp.StatusCode, string(body))
	}

	var locationsResp chrobinson.LoaderLocationsResponse
	if err := json.NewDecoder(resp.Body).Decode(&locationsResp); err != nil {
		return nil, fmt.Errorf("failed to parse loader API JSON: %w", err)
	}

	return locationsResp.Data, nil
}
