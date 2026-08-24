import Foundation
import CryptoKit
import Security

/// De koppelflow. Dit is het enige moment waarop de app met een server praat
/// zonder client-certificaat, en het enige moment waarop de agent iets zegt
/// tegen een onbekende client.
struct PairingInfo: Sendable, Equatable, Identifiable {
    var id: String { "\(host):\(port):\(code)" }

    var host: String
    var port: Int
    var caFingerprint: String
    var code: String
    var name: String

    /// Leest `nodestatus://enroll?h=…&p=…&fp=…&c=…&n=…` uit de QR.
    init?(url: URL) {
        guard url.scheme == "nodestatus", url.host == "enroll",
              let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems
        else { return nil }
        func q(_ k: String) -> String? { items.first { $0.name == k }?.value }
        guard let h = q("h"), let fp = q("fp"), let c = q("c") else { return nil }
        host = h
        port = Int(q("p") ?? "29500") ?? 29500
        caFingerprint = fp.lowercased()
        code = c.uppercased()
        name = q("n") ?? h
    }

    init(host: String, port: Int, caFingerprint: String, code: String, name: String) {
        self.host = host
        self.port = port
        self.caFingerprint = caFingerprint.lowercased()
        self.code = code.uppercased().replacingOccurrences(of: "-", with: "")
        self.name = name
    }
}

struct EnrollResponse: Codable, Sendable {
    var deviceId: String
    var clientCertPem: String
    var caCertPem: String
    var apiToken: String
    var expiresAt: Int64
    var hostname: String
    var displayName: String
}

/// Client voor precies één verzoek: POST /v1/enroll. Valideert de server aan
/// de hand van de CA-fingerprint uit de QR, nog vóór de koppelcode de deur
/// uit gaat.
final class EnrollmentClient: NSObject, URLSessionDelegate, @unchecked Sendable {

    private let expectedFingerprint: String

    // Elke poging krijgt een verse URLSession met een eigen connection pool.
    // Eén verbinding tegelijk (httpMaximumConnectionsPerHost = 1) voorkomt
    // dat URLSession zelf meerdere HTTP-verbindingen opent en racet, maar
    // Network.framework blijkt losstaand daarvan soms twee TLS-kandidaten op
    // TCP-niveau te racen voor dezelfde ene verbinding — de verliezer bedient
    // dan de underlying task, en hergebruik van diezelfde sessie/pool voor een
    // volgende poging bleek dat patroon te herhalen. Vandaar telkens een
    // volledig nieuwe sessie in plaats van één hergebruikte.
    private func makeSession() -> URLSession {
        let cfg = URLSessionConfiguration.ephemeral
        cfg.httpMaximumConnectionsPerHost = 1
        cfg.waitsForConnectivity = false
        return URLSession(configuration: cfg, delegate: self, delegateQueue: nil)
    }

    init(expectedFingerprint: String) {
        self.expectedFingerprint = expectedFingerprint.lowercased()
        super.init()
    }

    func enroll(_ info: PairingInfo, publicKey: Data, deviceName: String) async throws -> EnrollResponse {
        guard let url = URL(string: "https://\(info.host):\(info.port)/v1/enroll") else {
            throw APIClientError.badURL
        }
        var req = URLRequest(url: url)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.timeoutInterval = 20
        req.httpBody = try JSONEncoder().encode([
            "code": info.code,
            "public_key_b64": publicKey.base64EncodedString(),
            "device_name": deviceName,
        ])

        let (data, resp) = try await dataRetryingTransportErrors(for: req)
        guard let http = resp as? HTTPURLResponse else { throw APIClientError.http(0) }
        guard http.statusCode == 200 else {
            if let e = try? APIClient.decoder.decode(APIError.self, from: data) { throw e }
            throw APIClientError.http(http.statusCode)
        }
        let out = try APIClient.decoder.decode(EnrollResponse.self, from: data)

        // Tweede controle: de CA die we terugkrijgen moet dezelfde zijn als de
        // fingerprint uit de QR. Anders klopt er iets niet en gooien we alles weg.
        let ca = try PEM.certificate(from: out.caCertPem)
        guard Self.fingerprint(ca) == expectedFingerprint else {
            throw EnrollmentError.fingerprintMismatch
        }
        return out
    }

    /// De handshake met een net-gepairde server race intern soms twee
    /// verbindingskandidaten (zichtbaar als twee losse net.Conn's op de
    /// server, elk met een eigen TLS-handshake); als de verliezer de
    /// underlying task bedient krijgt de aanroeper een pure transportfout
    /// terwijl een tweede poging direct slaagt. Alleen transportfouten
    /// (TLS/verbinding) verdienen een retry — een fout ná een echte
    /// HTTP-response (verkeerde code, verlopen venster) blijft in één keer
    /// falen.
    private func dataRetryingTransportErrors(for req: URLRequest, attempts: Int = 8) async throws -> (Data, URLResponse) {
        var lastError: Error = APIClientError.badURL
        for attempt in 1...attempts {
            let session = makeSession()
            defer { session.finishTasksAndInvalidate() }
            do {
                return try await session.data(for: req)
            } catch let error as URLError {
                lastError = error
                switch error.code {
                case .secureConnectionFailed, .networkConnectionLost, .timedOut, .cannotConnectToHost:
                    if attempt < attempts {
                        try? await Task.sleep(nanoseconds: 150_000_000)
                        continue
                    }
                default:
                    throw error
                }
            }
        }
        throw lastError
    }

    static func fingerprint(_ cert: SecCertificate) -> String {
        let der = SecCertificateCopyData(cert) as Data
        return SHA256.hash(data: der).map { String(format: "%02x", $0) }.joined()
    }

    func urlSession(_ session: URLSession,
                    didReceive challenge: URLAuthenticationChallenge,
                    completionHandler: @escaping (URLSession.AuthChallengeDisposition, URLCredential?) -> Void) {
        // Tijdens enrollment vraagt de agent soms een optioneel client-cert
        // (mTLS "given, not required" tijdens een open koppelvenster). Er is
        // op dit moment geen certificaat voor déze server — .performDefaultHandling
        // bleek hier onbetrouwbaar: bij een enkel matchend item in de Keychain
        // (bijv. het certificaat van een al-gekoppelde server) selecteert iOS
        // dat soms zelf en biedt het aan, wat de agent terecht weigert (de
        // issuer-CA klopt niet). Expliciet weigeren voorkomt dat.
        if challenge.protectionSpace.authenticationMethod == NSURLAuthenticationMethodClientCertificate {
            completionHandler(.cancelAuthenticationChallenge, nil)
            return
        }
        guard challenge.protectionSpace.authenticationMethod == NSURLAuthenticationMethodServerTrust,
              let trust = challenge.protectionSpace.serverTrust else {
            completionHandler(.performDefaultHandling, nil)
            return
        }
        // De agent stuurt leaf + CA mee. Als een van beide overeenkomt met de
        // verwachte fingerprint, praten we met de juiste server.
        let chain = (SecTrustCopyCertificateChain(trust) as? [SecCertificate]) ?? []
        let match = chain.contains { Self.fingerprint($0) == expectedFingerprint }
        if match {
            completionHandler(.useCredential, URLCredential(trust: trust))
        } else {
            completionHandler(.cancelAuthenticationChallenge, nil)
        }
    }
}

enum EnrollmentError: LocalizedError {
    case fingerprintMismatch
    case wrongCode
    case noWindow

    var errorDescription: String? {
        switch self {
        case .fingerprintMismatch:
            "Het certificaat van de server komt niet overeen met de QR-code. Koppel niet verder."
        case .wrongCode:
            "De koppelcode klopt niet of is verlopen."
        case .noWindow:
            "Er staat geen koppelvenster open. Draai op de server: sudo nodestatus-agent enroll --new"
        }
    }
}
