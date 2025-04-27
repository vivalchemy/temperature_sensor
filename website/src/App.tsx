import { useState, useEffect, useRef } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ArrowUp, Calculator, Activity, Flame, Snowflake, Sun, ArrowDown } from "lucide-react";
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart"; // Shadcn Chart Components
import { CartesianGrid, Line, LineChart, XAxis, YAxis } from "recharts";
import { ModeToggle } from "./components/mode-toggle";
import { Badge } from "./components/ui/badge";
import { Button } from "./components/ui/button";
import { Tabs, TabsTrigger, TabsList } from "./components/ui/tabs";

// Replace your hardcoded constants with these lines
const STATS_URL = import.meta.env.VITE_STATS_URL;
const WEBSOCKET_URL = import.meta.env.VITE_WEBSOCKET_URL;

// TypeScript interfaces for our data
interface TimeSeriesPoint {
  timestamp: string;
  temperature: number;
}

type TimeRange = "1h" | "12h" | "1d" | "1w";

interface StatsResponse {
  mean: number;
  median: number;
  mode: number;
  highest: number;
  lowest: number;
}

interface WebSocketMessage {
  type: string;
  payload: any;
}

const TemperatureDashboard = () => {
  const [isFahrenheit, setIsFahrenheit] = useState(false);
  const [stats, setStats] = useState<StatsResponse>({
    mean: 0,
    median: 0,
    mode: 0,
    highest: 0,
    lowest: 0,
  });
  const [timeline, setTimeline] = useState<TimeSeriesPoint[]>([]);
  const [realtimeTemp, setRealtimeTemp] = useState<number | null>(null);
  const [connectionStatus, setConnectionStatus] = useState<"connected" | "disconnected" | "connecting">("connecting");
  const [range, setRange] = useState<TimeRange>("1h");
  const wsRef = useRef<WebSocket | null>(null);

  // Function to convert Celsius to Fahrenheit
  const toFahrenheit = (celsius: number) => (celsius * 9 / 5) + 32;

  // Function to format temperature based on selected unit
  const formatTemp = (temp: number | null) => {
    if (temp === null || temp === undefined) return "N/A";
    const temperature = isFahrenheit ? toFahrenheit(temp) : temp;
    return `${temperature.toFixed(1)}°${isFahrenheit ? 'F' : 'C'}`;
  };

  // Connect to WebSocket and set up event handlers
  useEffect(() => {
    const wsUrl = WEBSOCKET_URL;
    const connectWebSocket = () => {
      setConnectionStatus("connecting");
      wsRef.current = new WebSocket(wsUrl);

      wsRef.current.onopen = () => {
        console.log("Connected to WebSocket server");
        setConnectionStatus("connected");
      };

      // Inside wsRef.current.onmessage
      wsRef.current.onmessage = (event) => {
        try {
          const message = JSON.parse(event.data) as WebSocketMessage;

          const temp = message.payload.temperature;
          const timestamp = message.payload.timestamp;

          setRealtimeTemp(temp);

          // Optionally add to timeline if not already included in stats
          setTimeline((prevTimeline) => {
            const newTimeline = [...prevTimeline];
            const exists = newTimeline.some(point =>
              new Date(point.timestamp).getTime() === new Date(timestamp).getTime()
            );

            if (!exists) {
              newTimeline.push({
                timestamp: timestamp,
                temperature: temp
              });

              newTimeline.sort((a, b) =>
                new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime()
              );
            }

            return newTimeline;
          });
        } catch (error) {
          console.error("Error parsing WebSocket message:", error);
        }
      };

      wsRef.current.onerror = (error) => {
        console.error("WebSocket error:", error);
        setConnectionStatus("disconnected");
      };

      wsRef.current.onclose = () => {
        console.log("Disconnected from WebSocket server");
        setConnectionStatus("disconnected");
        setTimeout(connectWebSocket, 5000);
      };
    };

    connectWebSocket();

    return () => {
      if (wsRef.current) {
        wsRef.current.close();
      }
    };
  }, []);

  useEffect(() => {
    const fetchStats = async () => {
      try {
        const response = await fetch(STATS_URL);
        const data: StatsResponse = await response.json();
        setStats(data);
      } catch (error) {
        console.error("Error fetching stats:", error);
      }
    };

    // Fetch stats initially
    fetchStats();

    // Set interval to fetch stats every minute
    const intervalId = setInterval(fetchStats, 60000);

    // Clear interval on component unmount
    return () => {
      clearInterval(intervalId);
    };
  }, [range]);

  // Prepare chart data
  const chartData = timeline.map(point => ({
    timestamp: new Date(point.timestamp).toLocaleTimeString(),
    temperature: isFahrenheit ? toFahrenheit(point.temperature) : point.temperature
  }));

  const getTempIcon = (temp: number) => {
    if (temp > 30) {
      return <Flame size={48} className="mr-4 text-white" />;
    } else if (temp < 20) {
      return <Snowflake size={48} className="mr-4 text-white" />;
    } else {
      return <Sun size={48} className="mr-4 text-white" />;
    }
  };

  const getCardClasses = (temp: number) => {
    if (temp > 30) {
      return 'bg-gradient-to-r from-red-500 to-orange-500';
    } else if (temp < 20) {
      return 'bg-gradient-to-r from-blue-500 to-cyan-500';
    } else {
      return 'bg-gradient-to-r from-yellow-500 to-orange-400';
    }
  };

  const TemperatureCard = ({ realtimeTemp }: { realtimeTemp: number }) => {
    return (
      <Card className={`order-first md:order-0 text-white col-span-5 h-full ${getCardClasses(realtimeTemp)}`}>
        <CardContent className="pt-6">
          <div className="flex justify-center items-center">
            {getTempIcon(realtimeTemp)}
            <div className="text-4xl font-bold">{formatTemp(realtimeTemp)}</div>
          </div>
          <div className="text-center mt-2 text-white/80">
            {realtimeTemp > 30 ? 'Burning Hot!' : realtimeTemp < 20 ? 'Chilly Cold!' : 'Nice and Warm'}
          </div>
        </CardContent>
      </Card>
    );
  };

  return (
    <div className="p-6 max-w-6xl min-h-screen mx-auto">
      <div className="mb-6 flex flex-col gap-2 md:flex-row justify-between items-center">
        <div className="flex items-center gap-2">
          <h1 className="text-3xl font-bold">Temperature Monitor</h1>
          {connectionStatus === "connected" ? <Badge className="text-sm bg-green-500">Connected</Badge> :
            connectionStatus === "connecting" ? <Badge className="text-sm bg-blue-400">Connecting...</Badge> : <Badge className="text-sm bg-destructive">Disconnected</Badge>}
        </div>
        {/* Celsius/Fahrenheit toggle */}
        <div className="flex items-center gap-2">
          <div className="flex flex-col items-center gap-4">
            <Tabs
              value={range}
              onValueChange={(value: string) => setRange(value as TimeRange)}
            >
              <TabsList>
                <TabsTrigger value="1h">1h</TabsTrigger>
                <TabsTrigger value="12h">12h</TabsTrigger>
                <TabsTrigger value="1d">1d</TabsTrigger>
                <TabsTrigger value="1w">1w</TabsTrigger>
              </TabsList>
            </Tabs>

          </div>
          <Button onClick={() => setIsFahrenheit((prev) => !prev)} variant="outline">
            {isFahrenheit ? "°F" : "°C"}
          </Button>
          <ModeToggle />
        </div>
      </div>

      <div className="flex flex-col md:grid md:grid-cols-9 gap-4 mb-6">
        <Card className="col-span-2">
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium flex items-center">
              <ArrowUp className="mr-2 h-4 w-4" />
              Highest
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{formatTemp(stats.highest)}</div>
          </CardContent>
        </Card>

        <TemperatureCard realtimeTemp={realtimeTemp as number} />

        <Card className="col-span-2">
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium flex items-center">
              <ArrowDown className="mr-2 h-4 w-4" />
              Lowest
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{formatTemp(stats.lowest)}</div>
          </CardContent>
        </Card>
        <Card className="col-span-3">
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium flex items-center">
              <Calculator className="mr-2 h-4 w-4" />
              Mean
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{formatTemp(stats.mean)}</div>
          </CardContent>
        </Card>

        <Card className="col-span-3">
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium flex items-center">
              <Activity className="mr-2 h-4 w-4" />
              Median
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{formatTemp(stats.median)}</div>
          </CardContent>
        </Card>

        <Card className="col-span-3">
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium flex items-center">
              <Activity className="mr-2 h-4 w-4" />
              Mode
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{formatTemp(stats.mode)}</div>
          </CardContent>
        </Card>
      </div>

      {/* Temperature chart with Shadcn components */}
      <Card className="h-max">
        <CardHeader>
          <CardTitle>Temperature Timeline</CardTitle>
        </CardHeader>
        <CardContent>
          <ChartContainer
            id="temperature-chart"
            config={{}}
            className="h-full overflow-hidden"
          >
            <LineChart
              data={chartData}
              margin={{ top: 5, right: 30, left: 20, bottom: 5 }}
            >
              <CartesianGrid strokeDasharray="3 3" />
              <XAxis
                dataKey="timestamp"
                label={{ value: 'Time', position: 'insideBottomRight', offset: 0 }}
              />
              <YAxis
                label={{
                  value: `Temperature (°${isFahrenheit ? 'F' : 'C'})`,
                  angle: -90,
                  position: 'insideLeft'
                }}
              />
              <ChartTooltip content={<ChartTooltipContent />} />
              <Line
                type="monotone"
                dataKey="temperature"
                stroke="#8884d8"
                activeDot={{ r: 8 }}
              />
            </LineChart>
          </ChartContainer>
        </CardContent>
      </Card>
    </div >
  );
};

export default TemperatureDashboard;

