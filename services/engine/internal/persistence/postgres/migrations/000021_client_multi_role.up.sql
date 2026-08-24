-- Publisher and Agent developer are complementary client capabilities. Existing
-- client accounts receive both without changing privileged admin/arbitrator roles.
INSERT INTO user_roles (user_id, role)
SELECT user_id, 'agent_provider'
FROM user_roles
WHERE role = 'publisher'
ON CONFLICT DO NOTHING;

INSERT INTO user_roles (user_id, role)
SELECT user_id, 'publisher'
FROM user_roles
WHERE role = 'agent_provider'
ON CONFLICT DO NOTHING;
