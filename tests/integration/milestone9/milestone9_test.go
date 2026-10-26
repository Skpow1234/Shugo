package milestone9

import (
	"context"
	"testing"
	"time"

	"github.com/Skpow1234/Shugo/internal/blockchain"
	"github.com/Skpow1234/Shugo/internal/content"
	"github.com/Skpow1234/Shugo/internal/edge"
	"github.com/Skpow1234/Shugo/internal/iot"
	"github.com/Skpow1234/Shugo/internal/ipfs"
	"github.com/Skpow1234/Shugo/internal/ml"
)

func TestContentAddressing(t *testing.T) {
	contentAddresser := content.NewContentAddresser()

	// Test data
	testData := []byte("Hello, Shugo! This is a test file for content addressing.")

	// Test content ID generation
	contentID, err := contentAddresser.GenerateContentID(testData)
	if err != nil {
		t.Fatalf("Failed to generate content ID: %v", err)
	}

	if contentID.Hash == "" {
		t.Error("Content ID hash should not be empty")
	}

	if contentID.Algorithm != "sha256" {
		t.Errorf("Expected algorithm sha256, got %s", contentID.Algorithm)
	}

	if contentID.Size != int64(len(testData)) {
		t.Errorf("Expected size %d, got %d", len(testData), contentID.Size)
	}

	// Test CID generation
	cid, err := contentAddresser.GenerateCID(testData, "raw")
	if err != nil {
		t.Fatalf("Failed to generate CID: %v", err)
	}

	if cid.Hash == "" {
		t.Error("CID hash should not be empty")
	}

	if cid.Codec != "raw" {
		t.Errorf("Expected codec raw, got %s", cid.Codec)
	}

	// Test content verification
	valid, err := contentAddresser.VerifyContent(testData, contentID)
	if err != nil {
		t.Fatalf("Failed to verify content: %v", err)
	}

	if !valid {
		t.Error("Content verification should return true for matching data")
	}

	// Test content path generation
	path := contentAddresser.GetContentPath(contentID)
	if path == "" {
		t.Error("Content path should not be empty")
	}

	t.Logf("Content ID: %s", contentID.Hash)
	t.Logf("CID: %s", cid.Hash)
	t.Logf("Content Path: %s", path)
}

func TestIPFSCompatibility(t *testing.T) {
	ipfsCompat := ipfs.NewIPFSCompatibility()
	ctx := context.Background()

	// Test data
	testData := []byte("Hello, IPFS! This is a test file for IPFS compatibility.")

	// Test adding a block
	cid, err := ipfsCompat.AddBlock(ctx, testData, "raw")
	if err != nil {
		t.Fatalf("Failed to add block: %v", err)
	}

	if cid.Hash == "" {
		t.Error("CID hash should not be empty")
	}

	// Test retrieving a block
	block, err := ipfsCompat.GetBlock(ctx, cid)
	if err != nil {
		t.Fatalf("Failed to get block: %v", err)
	}

	if len(block.Data) != len(testData) {
		t.Errorf("Expected data length %d, got %d", len(testData), len(block.Data))
	}

	// Test pinning
	err = ipfsCompat.PinObject(ctx, cid, "test-pin", "recursive")
	if err != nil {
		t.Fatalf("Failed to pin object: %v", err)
	}

	// Test listing pins
	pins, err := ipfsCompat.ListPins(ctx)
	if err != nil {
		t.Fatalf("Failed to list pins: %v", err)
	}

	if len(pins) != 1 {
		t.Errorf("Expected 1 pin, got %d", len(pins))
	}

	// Test unpinning
	err = ipfsCompat.UnpinObject(ctx, cid)
	if err != nil {
		t.Fatalf("Failed to unpin object: %v", err)
	}

	// Test storage stats
	stats, err := ipfsCompat.GetStorageStats(ctx)
	if err != nil {
		t.Fatalf("Failed to get storage stats: %v", err)
	}

	if stats["blocks"].(int) != 1 {
		t.Errorf("Expected 1 block, got %d", stats["blocks"])
	}

	t.Logf("IPFS Block CID: %s", cid.Hash)
	t.Logf("Storage Stats: %+v", stats)
}

func TestBlockchainIntegration(t *testing.T) {
	blockchainIntegration := blockchain.NewBlockchainIntegration()
	ctx := context.Background()

	// Test adding a network
	network := &blockchain.BlockchainNetwork{
		Name:     "test-network",
		ChainID:  1337,
		RPCURL:   "http://localhost:8545",
		WSURL:    "ws://localhost:8546",
		Explorer: "https://explorer.test.com",
	}

	err := blockchainIntegration.AddNetwork(ctx, network)
	if err != nil {
		t.Fatalf("Failed to add network: %v", err)
	}

	// Test listing networks
	networks, err := blockchainIntegration.ListNetworks(ctx)
	if err != nil {
		t.Fatalf("Failed to list networks: %v", err)
	}

	if len(networks) != 1 {
		t.Errorf("Expected 1 network, got %d", len(networks))
	}

	// Test creating an identity
	t.Setenv("SHUGO_WALLET_KEY", "test-wallet-key")
	identity, err := blockchainIntegration.CreateIdentity(ctx, "test-network")
	if err != nil {
		t.Fatalf("Failed to create identity: %v", err)
	}

	if identity.DID == "" {
		t.Error("DID should not be empty")
	}

	if identity.Address == "" {
		t.Error("Address should not be empty")
	}

	// Test listing identities
	identities, err := blockchainIntegration.ListIdentities(ctx)
	if err != nil {
		t.Fatalf("Failed to list identities: %v", err)
	}

	if len(identities) != 1 {
		t.Errorf("Expected 1 identity, got %d", len(identities))
	}

	// Test deploying a contract
	contract := &blockchain.SmartContract{
		Address:  "0x1234567890123456789012345678901234567890",
		ABI:      `[{"inputs":[],"name":"getValue","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"}]`,
		Bytecode: "0x608060405234801561001057600080fd5b50600436106100365760003560e01c8063209652551461003b5780635524107714610059575b600080fd5b610043610075565b60405161005091906100a1565b60405180910390f35b61006161007b565b60405161006e91906100a1565b60405180910390f35b60005481565b60008054905090565b6000819050919050565b61009b81610088565b82525050565b60006020820190506100b66000830184610092565b9291505056fea2646970667358221220...",
		Name:     "TestContract",
		Version:  "1.0.0",
	}

	tx, err := blockchainIntegration.DeployContract(ctx, contract, "test-network")
	if err != nil {
		t.Fatalf("Failed to deploy contract: %v", err)
	}

	if tx.Hash == "" {
		t.Error("Transaction hash should not be empty")
	}

	// Test listing contracts
	contracts, err := blockchainIntegration.ListContracts(ctx)
	if err != nil {
		t.Fatalf("Failed to list contracts: %v", err)
	}

	if len(contracts) != 1 {
		t.Errorf("Expected 1 contract, got %d", len(contracts))
	}

	t.Logf("Created Identity DID: %s", identity.DID)
	t.Logf("Deployed Contract: %s", contract.Address)
	t.Logf("Transaction Hash: %s", tx.Hash)
}

func TestMachineLearning(t *testing.T) {
	mlEngine := ml.NewMLClassificationEngine()
	ctx := context.Background()

	// Test data
	testData := []byte("This is a test document for machine learning classification.")

	// Test file classification
	classification, err := mlEngine.ClassifyFile(ctx, "test.txt", testData, map[string]interface{}{
		"source": "test",
	})
	if err != nil {
		t.Fatalf("Failed to classify file: %v", err)
	}

	if classification.Category == "" {
		t.Error("Classification category should not be empty")
	}

	if classification.Confidence < 0 || classification.Confidence > 1 {
		t.Errorf("Confidence should be between 0 and 1, got %f", classification.Confidence)
	}

	// Test file optimization
	optimization, err := mlEngine.OptimizeFile(ctx, "test.txt", testData, "compression")
	if err != nil {
		t.Fatalf("Failed to optimize file: %v", err)
	}

	if optimization.OriginalSize != int64(len(testData)) {
		t.Errorf("Expected original size %d, got %d", len(testData), optimization.OriginalSize)
	}

	if optimization.CompressionRatio < 0 || optimization.CompressionRatio > 1 {
		t.Errorf("Compression ratio should be between 0 and 1, got %f", optimization.CompressionRatio)
	}

	// Test cache prediction
	accessHistory := []time.Time{
		time.Now().Add(-24 * time.Hour),
		time.Now().Add(-12 * time.Hour),
		time.Now().Add(-1 * time.Hour),
	}

	prediction, err := mlEngine.PredictCacheAccess(ctx, "test.txt", accessHistory, map[string]interface{}{
		"file_type": "document",
	})
	if err != nil {
		t.Fatalf("Failed to predict cache access: %v", err)
	}

	if prediction.AccessProbability < 0 || prediction.AccessProbability > 1 {
		t.Errorf("Access probability should be between 0 and 1, got %f", prediction.AccessProbability)
	}

	// Test model training
	model := &ml.MLModel{
		ID:      "test-model",
		Name:    "Test Model",
		Type:    "classification",
		Version: "1.0.0",
	}

	trainingData := []map[string]interface{}{
		{"extension": ".txt", "size": 1024, "label": "document"},
		{"extension": ".jpg", "size": 2048, "label": "image"},
	}

	err = mlEngine.TrainModel(ctx, model, trainingData)
	if err != nil {
		t.Fatalf("Failed to train model: %v", err)
	}

	if model.Accuracy < 0 || model.Accuracy > 1 {
		t.Errorf("Model accuracy should be between 0 and 1, got %f", model.Accuracy)
	}

	t.Logf("Classification: %s (%.2f%%)", classification.Category, classification.Confidence*100)
	t.Logf("Optimization: %.2f%% reduction", (1-optimization.CompressionRatio)*100)
	t.Logf("Cache Prediction: %.2f%% probability", prediction.AccessProbability*100)
	t.Logf("Model Accuracy: %.2f%%", model.Accuracy*100)
}

func TestEdgeComputing(t *testing.T) {
	edgeManager := edge.NewEdgeComputingManager()
	ctx := context.Background()

	// Test registering a node
	node := &edge.EdgeNode{
		ID:   "test-node",
		Name: "Test Edge Node",
		Location: &edge.Location{
			Latitude:  37.7749,
			Longitude: -122.4194,
			Altitude:  10.0,
			Address:   "123 Test St",
			City:      "San Francisco",
			Country:   "USA",
		},
		Capabilities: &edge.NodeCapabilities{
			CPU: &edge.CPUSpec{
				Cores:        4,
				Frequency:    2.4,
				Architecture: "x86_64",
				Usage:        25.0,
			},
			Memory: &edge.MemorySpec{
				Total:     8589934592, // 8GB
				Available: 6442450944, // 6GB
				Usage:     25.0,
			},
			Storage: &edge.StorageSpec{
				Total:     107374182400, // 100GB
				Available: 85899345920,  // 80GB
				Usage:     20.0,
				Type:      "SSD",
			},
			Network: &edge.NetworkSpec{
				Bandwidth: 1000000000, // 1Gbps
				Latency:   5.0,
				Protocols: []string{"TCP", "UDP", "HTTP"},
				IPAddress: "192.168.1.100",
			},
			Protocols: []string{"HTTP", "HTTPS", "gRPC"},
			Services:  []string{"compute", "storage"},
		},
		Status: "active",
	}

	err := edgeManager.RegisterNode(ctx, node)
	if err != nil {
		t.Fatalf("Failed to register node: %v", err)
	}

	// Test listing nodes
	nodes, err := edgeManager.ListNodes(ctx)
	if err != nil {
		t.Fatalf("Failed to list nodes: %v", err)
	}

	if len(nodes) != 1 {
		t.Errorf("Expected 1 node, got %d", len(nodes))
	}

	// Test submitting a task
	task := &edge.EdgeTask{
		ID:       "test-task",
		Name:     "Test Task",
		Type:     "compute",
		Priority: 1,
		Requirements: &edge.TaskRequirements{
			CPU:      2.0,
			Memory:   4294967296, // 4GB
			Storage:  1073741824, // 1GB
			Network:  100000000,  // 100Mbps
			GPU:      false,
			IoT:      false,
			Latency:  10.0,
			Duration: 5 * time.Minute,
		},
		Input: map[string]interface{}{
			"operation": "test",
		},
		Status: "pending",
	}

	err = edgeManager.SubmitTask(ctx, task)
	if err != nil {
		t.Fatalf("Failed to submit task: %v", err)
	}

	// Test listing tasks
	tasks, err := edgeManager.ListTasks(ctx)
	if err != nil {
		t.Fatalf("Failed to list tasks: %v", err)
	}

	if len(tasks) != 1 {
		t.Errorf("Expected 1 task, got %d", len(tasks))
	}

	// Test getting metrics
	metrics, err := edgeManager.GetMetrics(ctx)
	if err != nil {
		t.Fatalf("Failed to get metrics: %v", err)
	}

	if metrics.TotalNodes != 1 {
		t.Errorf("Expected 1 total node, got %d", metrics.TotalNodes)
	}

	if metrics.TotalTasks != 1 {
		t.Errorf("Expected 1 total task, got %d", metrics.TotalTasks)
	}

	t.Logf("Registered Node: %s", node.Name)
	t.Logf("Submitted Task: %s", task.Name)
	t.Logf("Metrics: %+v", metrics)
}

func TestIoTDevices(t *testing.T) {
	iotManager := iot.NewIoTManager()
	ctx := context.Background()

	// Test registering a device
	device := &iot.IoTDevice{
		ID:           "test-device",
		Name:         "Test IoT Device",
		Type:         "sensor",
		Manufacturer: "TestCorp",
		Model:        "TS-1000",
		Version:      "1.0.0",
		Status:       "active",
		Location: &iot.DeviceLocation{
			Latitude:  37.7749,
			Longitude: -122.4194,
			Altitude:  10.0,
			Room:      "Living Room",
			Building:  "Test Building",
			Floor:     1,
		},
		Capabilities: &iot.DeviceCapabilities{
			Sensors: []*iot.Sensor{
				{
					ID:           "temp-sensor",
					Name:         "Temperature Sensor",
					Type:         "temperature",
					Unit:         "celsius",
					Range:        &iot.SensorRange{Min: -40, Max: 85},
					Accuracy:     0.1,
					Resolution:   0.01,
					SamplingRate: 1.0,
					Status:       "active",
				},
			},
			Actuators: []*iot.Actuator{
				{
					ID:          "led-actuator",
					Name:        "LED Actuator",
					Type:        "led",
					ControlType: "pwm",
					Range:       &iot.ActuatorRange{Min: 0, Max: 255},
					Precision:   1.0,
					Status:      "active",
				},
			},
			Connectivity: &iot.Connectivity{
				WiFi: &iot.WiFiSpec{
					SSID:       "TestNetwork",
					BSSID:      "00:11:22:33:44:55",
					Frequency:  2.4,
					Channel:    6,
					Signal:     -45.0,
					Encryption: "WPA2",
					Protocols:  []string{"802.11n", "802.11ac"},
				},
			},
			Power: &iot.PowerSpec{
				Type:     "battery",
				Voltage:  3.7,
				Current:  0.1,
				Capacity: 2000.0,
				Level:    85.0,
				Status:   "good",
			},
		},
		Protocols: []string{"MQTT", "HTTP", "CoAP"},
	}

	err := iotManager.RegisterDevice(ctx, device)
	if err != nil {
		t.Fatalf("Failed to register device: %v", err)
	}

	// Test listing devices
	devices, err := iotManager.ListDevices(ctx)
	if err != nil {
		t.Fatalf("Failed to list devices: %v", err)
	}

	if len(devices) != 1 {
		t.Errorf("Expected 1 device, got %d", len(devices))
	}

	// Test sending sensor data
	sensorData := &iot.SensorData{
		DeviceID:  "test-device",
		SensorID:  "temp-sensor",
		Value:     22.5,
		Unit:      "celsius",
		Timestamp: time.Now(),
		Quality:   "good",
		Metadata: map[string]interface{}{
			"location": "living_room",
		},
	}

	err = iotManager.SendSensorData(ctx, sensorData)
	if err != nil {
		t.Fatalf("Failed to send sensor data: %v", err)
	}

	// Test sending actuator command
	actuatorCommand := &iot.ActuatorCommand{
		DeviceID:   "test-device",
		ActuatorID: "led-actuator",
		Command:    "set_brightness",
		Value:      128.0,
		Unit:       "pwm",
		Timestamp:  time.Now(),
		Priority:   1,
		Metadata: map[string]interface{}{
			"color": "white",
		},
	}

	err = iotManager.SendActuatorCommand(ctx, actuatorCommand)
	if err != nil {
		t.Fatalf("Failed to send actuator command: %v", err)
	}

	// Test getting metrics
	metrics, err := iotManager.GetMetrics(ctx)
	if err != nil {
		t.Fatalf("Failed to get metrics: %v", err)
	}

	if metrics.TotalDevices != 1 {
		t.Errorf("Expected 1 total device, got %d", metrics.TotalDevices)
	}

	if metrics.TotalSensors != 1 {
		t.Errorf("Expected 1 total sensor, got %d", metrics.TotalSensors)
	}

	if metrics.TotalActuators != 1 {
		t.Errorf("Expected 1 total actuator, got %d", metrics.TotalActuators)
	}

	if metrics.DataPoints != 1 {
		t.Errorf("Expected 1 data point, got %d", metrics.DataPoints)
	}

	if metrics.CommandsSent != 1 {
		t.Errorf("Expected 1 command sent, got %d", metrics.CommandsSent)
	}

	t.Logf("Registered Device: %s", device.Name)
	t.Logf("Sensor Data: %.1f %s", sensorData.Value, sensorData.Unit)
	t.Logf("Actuator Command: %s %.1f", actuatorCommand.Command, actuatorCommand.Value)
	t.Logf("Metrics: %+v", metrics)
}
