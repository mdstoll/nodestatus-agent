import Foundation

/// Decodeert `null` als een lege array. De agent hoort altijd `[]` te sturen,
/// maar de app moet niet stukgaan op een oudere agent die `null` stuurt —
/// één ontbrekende GPU-lijst mag nooit het hele scherm leeg laten.
@propertyWrapper
struct DefaultEmpty<T: Codable & Sendable & Equatable>: Codable, Sendable, Equatable {
    var wrappedValue: [T]
    init(wrappedValue: [T] = []) { self.wrappedValue = wrappedValue }
    init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        wrappedValue = (try? c.decode([T].self)) ?? []
    }
    func encode(to encoder: Encoder) throws { try wrappedValue.encode(to: encoder) }
}

// Modellen komen exact overeen met het API-contract in docs/03-api-contract.md.
// Alles wat elke seconde binnenkomt is een struct (value type): goedkoop te
// kopiëren en veilig over actorgrenzen.

// MARK: - Statische systeeminformatie

struct SystemInfo: Codable, Sendable, Equatable {
    var hostname: String
    var displayName: String
    var os: OSInfo
    var model: ModelInfo
    var cpu: CPUInfo
    var memoryTotal: UInt64
    var swapTotal: UInt64
    var storageTotalBytes: UInt64
    var bootTime: Int64
    var uptimeS: Double
    var capabilities: [String]
    var agentVersion: String

    struct OSInfo: Codable, Sendable, Equatable {
        var name: String
        var version: String
        var id: String
        var kernel: String
        var arch: String
        var virtualization: String?
    }

    struct ModelInfo: Codable, Sendable, Equatable {
        var vendor: String
        var product: String
        var board: String?
    }

    struct CPUInfo: Codable, Sendable, Equatable {
        var model: String
        var vendor: String
        var coresPhysical: Int
        var threads: Int
        var sockets: Int
        var maxMhz: Int?
        var cacheL3Bytes: UInt64?
        var flagsNotable: [String]?
        var governor: String?
    }

    /// T("Model", "Model") in de kop van het Metrics-scherm. In een VM staat er geen DMI-naam,
    /// dan tonen we het virtualisatietype in plaats van een leeg veld.
    var modelLine: String {
        let p = model.product.trimmingCharacters(in: .whitespaces)
        if !p.isEmpty { return p }
        if let v = os.virtualization, !v.isEmpty { return v.uppercased() }
        return os.arch
    }

    var osLine: String {
        let v = os.version.isEmpty ? "" : " \(os.version)"
        return "\(os.name)\(v)"
    }

    func has(_ capability: String) -> Bool { capabilities.contains(capability) }
    var hasSpeedtest: Bool { capabilities.contains { $0.hasPrefix("speedtest.") } }
    var hasGPU: Bool { capabilities.contains { $0.hasPrefix("gpu.") } }
}

// MARK: - Live sample

struct Sample: Codable, Sendable, Equatable, Identifiable {
    var t: Double
    var cpu: CPU
    var memory: Memory
    @DefaultEmpty var storage: [Storage]
    var network: Network
    @DefaultEmpty var temps: [Temp]
    @DefaultEmpty var gpu: [GPU]

    var id: Double { t }

    struct CPU: Codable, Sendable, Equatable {
        var total: Double
        var user: Double
        var system: Double
        var iowait: Double
        var steal: Double
        @DefaultEmpty var cores: [Double]
        var freqMhz: [Int]?
        var load: [Double]
        var procsRunning: Int
        var procsTotal: Int

        var loadLine: String {
            load.map { String(format: "%.2f", $0) }.joined(separator: " / ")
        }

        /// Kortere variant voor de tegel: daar past de volledige triple niet
        /// op één regel naast het grote getal.
        var loadShort: String {
            guard load.count >= 3 else { return loadLine }
            return String(format: "%.2f · %.2f", load[1], load[2])
        }
    }

    struct Memory: Codable, Sendable, Equatable {
        var total: UInt64
        var used: UInt64
        var available: UInt64
        var cached: UInt64
        var buffers: UInt64
        var swapTotal: UInt64
        var swapUsed: UInt64
        var percent: Double
    }

    struct Storage: Codable, Sendable, Equatable, Identifiable {
        var mount: String
        var device: String
        var fstype: String
        var total: UInt64
        var used: UInt64
        var percent: Double
        var remote: Bool
        var readBps: UInt64
        var writeBps: UInt64

        var id: String { mount }
    }

    struct Network: Codable, Sendable, Equatable {
        var rxBps: UInt64
        var txBps: UInt64
        var rxTotal: UInt64
        var txTotal: UInt64
        @DefaultEmpty var interfaces: [Interface]

        struct Interface: Codable, Sendable, Equatable, Identifiable {
            var name: String
            var up: Bool
            var speedMbps: Int?
            var virtual: Bool
            var rxBps: UInt64
            var txBps: UInt64
            var rxTotal: UInt64
            var txTotal: UInt64

            var id: String { name }
        }

        var primaryInterface: Interface? {
            interfaces.first { !$0.virtual && $0.up } ?? interfaces.first { !$0.virtual }
        }
    }

    struct Temp: Codable, Sendable, Equatable, Identifiable {
        var key: String
        var label: String
        var chip: String
        var celsius: Double
        var high: Double?
        var critical: Double?
        var status: Status
        var primary: Bool?

        var id: String { key }
    }

    struct GPU: Codable, Sendable, Equatable, Identifiable {
        var index: Int
        var vendor: String
        var name: String
        var driver: String?
        var utilPercent: Double
        var memUsed: UInt64
        var memTotal: UInt64
        var sharedMemory: Bool?
        var tempC: Double?
        var powerW: Double?
        var fanPercent: Double?
        var clockMhz: Int?
        var clockMaxMhz: Int?
        var engines: [Engine]?
        var note: String?

        var id: Int { index }

        /// Een geïntegreerde GPU deelt het systeemgeheugen; een VRAM-balk
        /// tonen zou daar een verkeerd beeld van geven.
        var hasOwnMemory: Bool { memTotal > 0 && sharedMemory != true }

        struct Engine: Codable, Sendable, Equatable, Identifiable {
            var name: String
            var busy: Double
            var id: String { name }
        }
    }

    /// Lokale opslag telt mee in de samenvatting; netwerkmounts niet — anders
    /// zou een gemounte clouddrive van 45 TB het totaal domineren.
    var localStorage: [Storage] { storage.filter { !$0.remote } }
    var remoteStorage: [Storage] { storage.filter { $0.remote } }

    var storageTotal: UInt64 { localStorage.reduce(0) { $0 + $1.total } }
    var storageUsed: UInt64 { localStorage.reduce(0) { $0 + $1.used } }
    var storagePercent: Double {
        storageTotal > 0 ? Double(storageUsed) / Double(storageTotal) * 100 : 0
    }

    var primaryTemp: Temp? {
        temps.first { $0.primary == true } ?? temps.max { $0.celsius < $1.celsius }
    }
}

// MARK: - Hardware & tools

struct SensorsResult: Codable, Sendable {
    var chips: [Chip]
    var available: Int
    var unavailable: Int

    struct Chip: Codable, Sendable, Identifiable {
        var name: String
        var sensors: [Sensor]
        var id: String { name }
    }

    struct Sensor: Codable, Sendable, Identifiable {
        var key: String
        var label: String
        var type: String
        var value: Double
        var unit: String
        var high: Double?
        var critical: Double?
        var status: Status
        var id: String { key }

        var formatted: String {
            switch type {
            case "temperature": String(format: "%.0f %@", value, unit)
            case "fan":         String(format: "%.0f %@", value, unit)
            default:            String(format: "%.2f %@", value, unit)
            }
        }

        var symbol: String {
            switch type {
            case "temperature": "thermometer.medium"
            case "fan":         "fan.fill"
            case "voltage":     "bolt.fill"
            case "power":       "powerplug.fill"
            case "current":     "waveform.path.ecg"
            default:            "sensor.fill"
            }
        }
    }
}

struct SmartResult: Codable, Sendable {
    var disks: [Disk]
    struct Disk: Codable, Sendable, Identifiable {
        var device: String
        var model: String
        var sizeBytes: UInt64
        var protocolName: String?
        var rotationRpm: Int?
        var health: String
        var tempC: Int?
        var powerOnHours: Int?
        var powerCycles: Int?
        var percentageUsed: Int?
        var error: String?
        var id: String { device }

        var isSSD: Bool { (rotationRpm ?? 0) == 0 }
        var healthStatus: Status {
            switch health {
            case "PASSED": .ok
            case "FAILED": .crit
            default: .warn
            }
        }

        // Let op: de decoder draait met .convertFromSnakeCase, dus de sleutels
        // zijn hier al camelCase. Alleen "protocol" heeft een eigen naam nodig
        // omdat het een gereserveerd woord is in Swift.
        enum CodingKeys: String, CodingKey {
            case device, model, health, error
            case sizeBytes, rotationRpm, tempC, powerOnHours, powerCycles, percentageUsed
            case protocolName = "protocol"
        }
    }
}

struct DisksResult: Codable, Sendable {
    var devices: [Device]
    struct Device: Codable, Sendable, Identifiable {
        var name: String
        var path: String
        var size: UInt64
        var type: String
        var model: String?
        var rotational: Bool
        var fstype: String?
        var mount: String?
        var children: [Device]?
        var id: String { path }
    }
}

struct NetworkResult: Codable, Sendable {
    var interfaces: [NIC]
    var gateway: String?
    var dns: [String]?
    struct NIC: Codable, Sendable, Identifiable {
        var name: String
        var mac: String
        var mtu: Int
        var speedMbps: Int?
        var state: String
        var addresses: [String]
        var virtual: Bool
        var id: String { name }
    }
}

struct UptimeInfo: Codable, Sendable {
    var uptimeS: Double
    var bootTime: Int64
    var idleS: Double
    var busyRatio: Double
    var cpuTime: CPUTime
    var boots: [Boot]

    struct CPUTime: Codable, Sendable {
        var user: Double
        var system: Double
        var iowait: Double
        var idle: Double
    }
    struct Boot: Codable, Sendable, Identifiable {
        var index: Int
        var start: String
        var stop: String?
        var id: Int { index }
    }
}

struct LocaleInfo: Codable, Sendable {
    var localeIdentifier: String
    var language: String
    var region: String
    var preferredLanguages: [String]
    var keyboardLayout: String?
    var timeZone: String
    var utcOffset: String
    var localTime: String
    var ntpSynchronized: Bool
    var rtcInLocalTz: Bool
    var calendar: String
    var firstDayOfWeek: String
    var hourCycle: String
}

struct UpdatesInfo: Codable, Sendable {
    var upgradable: Int
    var security: Int
    var rebootRequired: Bool
    var rebootRequiredPkgs: [String]
    var unattendedUpgrades: Bool
    var lastAptUpdate: Int64?
    var packages: [Package]
    var error: String?

    struct Package: Codable, Sendable, Identifiable {
        var name: String
        var current: String
        var candidate: String
        var security: Bool
        var id: String { name }
    }
}

struct ProcessesResult: Codable, Sendable {
    var summary: Summary
    var processes: [Proc]
    var zombieParents: [ZombieParent]?

    struct Summary: Codable, Sendable {
        var total: Int
        var running: Int
        var sleeping: Int
        var stopped: Int
        var zombie: Int
        var threads: Int
    }

    struct ZombieParent: Codable, Sendable, Identifiable {
        var pid: Int
        var name: String
        var count: Int
        var id: Int { pid }
    }

    struct Proc: Codable, Sendable, Identifiable {
        var pid: Int
        var name: String
        var user: String
        var cpuPercent: Double
        var rss: UInt64
        var memPercent: Double
        var state: String
        var threads: Int
        var id: Int { pid }
    }
}

struct LogSourcesResult: Codable, Sendable {
    var sources: [Source]
    struct Source: Codable, Sendable, Identifiable {
        var id: String
        var label: String
        var kind: String
        var available: Bool
    }
}

struct LogsResult: Codable, Sendable {
    var source: String
    var lines: [Line]
    struct Line: Codable, Sendable, Identifiable {
        var t: Double
        var priority: Int
        var unit: String?
        var pid: Int?
        var message: String
        var id: String { "\(t)-\(message.hashValue)" }

        var level: String {
            switch priority {
            case 0...3: "ERR"
            case 4: "WARN"
            case 5: "NOTE"
            case 7: "DBG"
            default: "INFO"
            }
        }
        var status: Status {
            switch priority {
            case 0...3: .crit
            case 4: .warn
            default: .ok
            }
        }
    }
}

struct CPUInfoResult: Codable, Sendable {
    var current: Sample.CPU
    var staticInfo: SystemInfo.CPUInfo
    var history: [Point]
    struct Point: Codable, Sendable {
        var t: Double
        var total: Double
        var user: Double
        var system: Double
    }
    enum CodingKeys: String, CodingKey {
        case current, history
        case staticInfo = "static"
    }
}

struct DevicesResult: Codable, Sendable {
    var devices: [Device]
    struct Device: Codable, Sendable, Identifiable {
        var id: String
        var name: String
        var enrolledAt: Int64
        var lastSeen: Int64
        var expiresAt: Int64
        var isCurrent: Bool
    }
}

// MARK: - Jobs

struct JobStatus: Codable, Sendable {
    var jobId: String
    var type: String
    var state: String
    var phase: String?
    var progress: Double
    var error: String?

    var isFinished: Bool { state == "done" || state == "failed" }
}

struct SpeedtestResult: Codable, Sendable {
    var downloadBps: Double
    var uploadBps: Double
    var pingMs: Double
    var jitterMs: Double
    var packetLoss: Double
    var serverName: String
    var serverCity: String?
    var isp: String?
    var externalIpMasked: String?
    var resultUrl: String?
    var engine: String
}

struct PingResult: Codable, Sendable {
    var target: String
    var resolvedIp: String?
    var sent: Int
    var received: Int
    var lossPercent: Double
    var minMs: Double
    var avgMs: Double
    var maxMs: Double
    var mdevMs: Double
    var rttsMs: [Double]
}

struct DNSResult: Codable, Sendable {
    var query: String
    var record: String
    var server: String
    var answers: [Answer]
    var queryMs: Double
    struct Answer: Codable, Sendable, Identifiable {
        var name: String
        var type: String
        var ttl: Int
        var value: String
        var id: String { "\(name)-\(type)-\(value)" }
    }
}

struct WhoisResult: Codable, Sendable {
    var query: String
    var registrar: String?
    var created: String?
    var expires: String?
    var updated: String?
    var nameServers: [String]
    var status: [String]
    var raw: String
}

struct TracerouteResult: Codable, Sendable {
    var target: String
    var hops: [Hop]
    struct Hop: Codable, Sendable, Identifiable {
        var number: Int
        var host: String
        var ip: String?
        var rttsMs: [Double]
        var id: Int { number }
        var avg: Double? { rttsMs.isEmpty ? nil : rttsMs.reduce(0, +) / Double(rttsMs.count) }
    }
}

struct APIError: Codable, Sendable, Error, LocalizedError {
    struct Body: Codable, Sendable {
        var code: String
        var message: String
    }
    var error: Body
    var errorDescription: String? { error.message }
}

struct UpdateCheckResult: Codable, Sendable {
    var current: String
    var latest: String?
    var releaseUrl: String?
    var available: Bool
}
