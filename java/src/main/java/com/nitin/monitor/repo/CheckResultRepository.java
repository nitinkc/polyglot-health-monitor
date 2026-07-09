package com.nitin.monitor.repo;

import com.nitin.monitor.dto.CheckResult;
import java.sql.Connection;
import java.sql.PreparedStatement;
import java.sql.ResultSet;
import java.sql.SQLException;
import java.time.Instant;
import java.util.ArrayList;
import java.util.List;

public class CheckResultRepository {
    private final Connection conn;

    public CheckResultRepository(Connection conn) {
        this.conn = conn;
    }

    public void insert(CheckResult r) {
        String sql = """
            INSERT INTO check_results
                (id, monitor_id, checked_at, success, status_code, latency_ms, error)
            VALUES (?, ?, ?, ?, ?, ?, ?)
        """;
        try (PreparedStatement ps = conn.prepareStatement(sql)) {
            ps.setString(1, r.id);
            ps.setString(2, r.monitorId);
            ps.setString(3, r.checkedAt.toString());
            ps.setInt(4, r.success ? 1 : 0);
            if (r.statusCode != null)
                ps.setInt(5, r.statusCode);
            else
                ps.setNull(5, java.sql.Types.INTEGER);
            if (r.latencyMs != null)
                ps.setInt(6, r.latencyMs);
            else
                ps.setNull(6, java.sql.Types.INTEGER);
            ps.setString(7, r.error);
            ps.executeUpdate();
        } catch (SQLException e) {
            throw new RuntimeException("failed to insert check result for monitor " + r.monitorId, e);
        }
    }

    /** Newest-first, capped at `limit` (spec: default 20, max 100 — enforce at the route layer). */
    public List<CheckResult> recentForMonitor(String monitorId, int limit) {
        String sql = """
            SELECT * FROM check_results
            WHERE monitor_id = ?
            ORDER BY checked_at DESC
            LIMIT ?
        """;
        List<CheckResult> out = new ArrayList<>();
        try (PreparedStatement ps = conn.prepareStatement(sql)) {
            ps.setString(1, monitorId);
            ps.setInt(2, limit);
            try (ResultSet rs = ps.executeQuery()) {
                while (rs.next()) {
                    CheckResult r = new CheckResult();
                    r.id = rs.getString("id");
                    r.monitorId = rs.getString("monitor_id");
                    r.checkedAt = Instant.parse(rs.getString("checked_at"));
                    r.success = rs.getInt("success") == 1;
                    int statusCode = rs.getInt("status_code");
                    r.statusCode = rs.wasNull() ? null : statusCode;
                    int latencyMs = rs.getInt("latency_ms");
                    r.latencyMs = rs.wasNull() ? null : latencyMs;
                    r.error = rs.getString("error");
                    out.add(r);
                }
            }
        } catch (SQLException e) {
            throw new RuntimeException("failed to fetch history for monitor " + monitorId, e);
        }
        return out;
    }
}
