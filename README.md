# Temperature Monitor

A real-time temperature monitoring dashboard with data processing, statistics, and visualization capabilities.

![Temperature Monitor Dashboard](assets/demo.png)

## 🌡️ Project Overview

Temperature Monitor is a distributed system that collects, processes, and visualizes temperature data in real-time. The project consists of multiple microservices working together to provide a seamless monitoring experience.


## 👥 Contributors

- [Rohit Patra](https://github.com/Rohitpatra007/)
- [Mark Lopes](https://github.com/MarkLopes11/)


### Key Features

- **Real-time Temperature Monitoring**: Live updates via WebSocket connections
- **Statistical Analysis**: Calculate mean, median, mode, and extremes
- **Interactive Dashboard**: Beautiful UI with temperature visualizations
- **Unit Conversion**: Toggle between Celsius and Fahrenheit
- **Time Range Selection**: View data from the last hour to the last week

## 🏗️ Architecture

The project consists of the following components:

- **WebSocket Server**: Handles real-time data streaming
- **Alert Lambda**: Processes temperature alerts based on thresholds
- **Stats Lambda**: Calculates statistical data about temperatures
- **Website**: React-based UI for data visualization
- **Dummy Sensor**: Simulates temperature data for testing

## 🛠️ Technologies Used

- **Backend**:
  - Go (WebSocket server, dummy sensors)
  - Python (Lambda functions with FastAPI)
  - SQLite (Data storage)
  - SQLC (SQL query generation)
  
- **Frontend**:
  - React
  - TypeScript
  - Recharts (data visualization)
  - Bun (JavaScript runtime/bundler)
  - Shadcn UI components

## 🚀 Getting Started

### Prerequisites

- Go 1.19+
- Python 3.10+
- Bun
- uv (Python package manager)
- sqlc
- air (for live reloading)

### Setup and Installation

Clone the repository:

```bash
git clone https://github.com/your-username/temperature-monitor.git
cd temperature-monitor
```

Generate all required files:

```bash
make gen
```

This command will:
- Generate database queries for the WebSocket server
- Set up virtual environments for both lambda functions
- Install dependencies for the website
- Prepare the dummy sensor

### Running in Development Mode

Start all services in development mode with hot-reloading:

```bash
make dev/websocket      # Start WebSocket server
make dev/alert-lambda   # Start alert processing service
make dev/stats-lambda   # Start statistics service
make dev/website        # Start frontend development server
make dev/dummy-sensor   # Start dummy data generator
```

Or run individual services as needed.

### Building for Production

Build all components:

```bash
make build
```

This creates optimized builds for the WebSocket server, dummy sensor, and website.

### Running in Production Mode

Start all services in production mode:

```bash
make preview/websocket
make preview/alert-lambda
make preview/stats-lambda
make preview/website
make preview/dummy-sensor
```

## 📊 Dashboard Features

The temperature dashboard provides:

- Current temperature display with visual indicators
- Historical temperature chart
- Statistical data (mean, median, mode, highest, lowest)
- Time range selection (1h, 12h, 1d, 1w)
- Unit toggle (°C/°F)
- Dark/light mode toggle
- WebSocket connection status indicator

## 🧹 Cleaning Up

To clean all generated files and build artifacts:

```bash
make clean
```

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## 📝 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---

Made with ❤️ for temperature enthusiasts everywhere
