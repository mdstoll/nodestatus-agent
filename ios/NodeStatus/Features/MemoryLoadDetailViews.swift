import SwiftUI

// MARK: - RAM

/// A closer look at memory: where it's going (used / cached / buffers /
/// free, the same breakdown `btop` shows), swap, and how usage has moved
/// over the last few minutes.
struct RAMDetailView: View {
    @Environment(AppState.self) private var app

    var body: some View {
        DetailScroll {
            if let s = app.latest {
                Card {
                    VStack(spacing: 14) {
                        HStack(alignment: .firstTextBaseline) {
                            Text(Fmt.bytes(s.memory.used, binary: app.prefs.binaryUnits))
                                .font(.system(size: 34, weight: .bold, design: .rounded))
                                .monospacedDigit()
                                .foregroundStyle(Theme.C.text)
                            Text("/ \(Fmt.bytes(s.memory.total, binary: app.prefs.binaryUnits))")
                                .font(.subheadline)
                                .foregroundStyle(Theme.C.textSecondary)
                            Spacer()
                            Text(Fmt.percent(s.memory.percent))
                                .font(.title2.bold().monospacedDigit())
                                .foregroundStyle(Status.forPercent(s.memory.percent).color)
                        }
                        GaugeBar(fraction: s.memory.percent / 100, gradient: Theme.G.ram, height: 8)
                    }
                }

                Card {
                    VStack(spacing: 14) {
                        DonutChart(slices: breakdown(s).map {
                            .init(label: $0.0, value: max($0.1, 0), color: $0.2)
                        })
                        .frame(maxWidth: .infinity)

                        VStack(spacing: 8) {
                            ForEach(breakdown(s), id: \.0) { label, value, color in
                                legendRow(label, color, value)
                            }
                        }
                    }
                }

                Card {
                    VStack(alignment: .leading, spacing: 10) {
                        Text(T("History", "Geschiedenis")).font(.headline).foregroundStyle(Theme.C.text)
                        Sparkline(values: history(), color: Theme.C.blue, height: 70)
                    }
                }

                InfoCard(title: "Swap", symbol: "arrow.left.arrow.right", tint: Theme.C.gray) {
                    if s.memory.swapTotal == 0 {
                        InfoRow(label: T("Swap", "Swap"), value: T("none", "geen"))
                    } else {
                        InfoRow(label: T("Used", "In gebruik"),
                                value: "\(Fmt.bytes(s.memory.swapUsed, binary: app.prefs.binaryUnits)) / \(Fmt.bytes(s.memory.swapTotal, binary: app.prefs.binaryUnits))")
                    }
                }
            } else {
                ProgressView().frame(maxWidth: .infinity, minHeight: 200)
            }
        }
        .navigationTitle("RAM")
    }

    /// Same convention as `btop`: Used is what the agent already reports
    /// (total − available, i.e. not reclaimable), Cached and Buffers are
    /// reclaimable, and Free is whatever's left over.
    private func breakdown(_ s: Sample) -> [(String, Double, Color)] {
        let total = Double(s.memory.total)
        guard total > 0 else { return [] }
        let used = Double(s.memory.used)
        let cached = Double(s.memory.cached)
        let buffers = Double(s.memory.buffers)
        let free = max(total - used - cached - buffers, 0)
        return [
            (T("Used", "In gebruik"), used, Theme.C.blue),
            (T("Cached", "Cache"), cached, Theme.C.cyan),
            (T("Buffers", "Buffers"), buffers, Theme.C.purple),
            (T("Free", "Vrij"), free, Theme.C.cardElevated),
        ]
    }

    private func legendRow(_ label: String, _ color: Color, _ bytes: Double) -> some View {
        HStack(spacing: 8) {
            RoundedRectangle(cornerRadius: 2).fill(color).frame(width: 10, height: 10)
            Text(label).font(.footnote).foregroundStyle(Theme.C.textSecondary)
            Spacer()
            Text(Fmt.bytes(UInt64(bytes), binary: app.prefs.binaryUnits))
                .font(.footnote.monospacedDigit())
                .foregroundStyle(Theme.C.text)
        }
    }

    private func history() -> [Double] {
        let v = app.history.elements.map(\.memory.percent)
        return v.isEmpty ? [0] : v
    }
}

// MARK: - Load

/// Load average over time. On its own a single number ("0.61") means little
/// without knowing whether it's climbing or already coming back down — the
/// history line is the point of this screen.
struct LoadDetailView: View {
    @Environment(AppState.self) private var app

    var body: some View {
        DetailScroll {
            if let s = app.latest {
                Card {
                    VStack(alignment: .leading, spacing: 14) {
                        HStack(alignment: .firstTextBaseline) {
                            Text(String(format: "%.2f", s.cpu.load.first ?? 0))
                                .font(.system(size: 34, weight: .bold, design: .rounded))
                                .monospacedDigit()
                                .foregroundStyle(Theme.C.text)
                            Text(T("1 min average", "1 min gemiddelde"))
                                .font(.subheadline)
                                .foregroundStyle(Theme.C.textSecondary)
                            Spacer()
                        }
                        GaugeBar(fraction: loadFraction(s), gradient: Theme.G.load, height: 8)
                        Text(T("Relative to \(max(s.cpu.cores.count, 1)) threads — 1.0 per thread is 100% busy.",
                               "Relatief aan \(max(s.cpu.cores.count, 1)) threads — 1,0 per thread is 100% bezet."))
                            .font(.caption2)
                            .foregroundStyle(Theme.C.textTertiary)
                    }
                }

                HStack(spacing: Theme.M.cardGap) {
                    loadStat(T("1 min", "1 min"), s.cpu.load.count > 0 ? s.cpu.load[0] : 0)
                    loadStat(T("5 min", "5 min"), s.cpu.load.count > 1 ? s.cpu.load[1] : 0)
                    loadStat(T("15 min", "15 min"), s.cpu.load.count > 2 ? s.cpu.load[2] : 0)
                }

                Card {
                    VStack(alignment: .leading, spacing: 10) {
                        Text(T("History", "Geschiedenis")).font(.headline).foregroundStyle(Theme.C.text)
                        Sparkline(values: history(), color: Theme.C.green, height: 70)
                    }
                }
            } else {
                ProgressView().frame(maxWidth: .infinity, minHeight: 200)
            }
        }
        .navigationTitle(T("Load", "Load"))
    }

    private func loadFraction(_ s: Sample) -> Double {
        let threads = max(s.cpu.cores.count, 1)
        return min((s.cpu.load.first ?? 0) / Double(threads), 1)
    }

    private func loadStat(_ label: String, _ value: Double) -> some View {
        Card {
            VStack(alignment: .leading, spacing: 4) {
                Text(label).font(.caption).foregroundStyle(Theme.C.textSecondary)
                Text(String(format: "%.2f", value))
                    .font(.title3.bold().monospacedDigit())
                    .foregroundStyle(Theme.C.text)
            }
        }
    }

    private func history() -> [Double] {
        let v = app.history.elements.compactMap { $0.cpu.load.first }
        return v.isEmpty ? [0] : v
    }
}
