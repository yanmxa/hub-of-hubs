--- the CREATE TRIGGER only for postgre 14
--- CREATE TRIGGER set_timestamp BEFORE UPDATE ON history.applications FOR EACH ROW EXECUTE FUNCTION public.trigger_set_timestamp();

-- set the cluster_id to compliance table
DROP TRIGGER IF EXISTS update_compliance_table ON local_status.compliance;
CREATE TRIGGER update_compliance_table AFTER INSERT OR UPDATE ON local_status.compliance FOR EACH ROW WHEN (pg_trigger_depth() < 1) EXECUTE FUNCTION public.set_cluster_id_to_local_compliance();

-- update the compliance cluster_id when insert record to managed clusters
DROP TRIGGER IF EXISTS update_local_compliance_cluster_id_trigger ON status.managed_clusters;
CREATE TRIGGER update_local_compliance_cluster_id_trigger
AFTER INSERT OR UPDATE ON status.managed_clusters
FOR EACH ROW
EXECUTE FUNCTION public.update_local_compliance_cluster_id();

--- create the current month partitioned tables for local_policies and local_root_policies
SELECT create_monthly_range_partitioned_table('event.local_root_policies', to_char(current_date, 'YYYY-MM-DD'));
SELECT create_monthly_range_partitioned_table('event.local_policies', to_char(current_date, 'YYYY-MM-DD'));
SELECT create_monthly_range_partitioned_table('history.local_compliance', to_char(current_date, 'YYYY-MM-DD'));
SELECT create_monthly_range_partitioned_table('event.managed_clusters', to_char(current_date, 'YYYY-MM-DD'));

--- create the previous month partitioned tables for receiving the data from the previous month
SELECT create_monthly_range_partitioned_table('event.local_root_policies', to_char(current_date - interval '1 month', 'YYYY-MM-DD'));
SELECT create_monthly_range_partitioned_table('event.local_policies', to_char(current_date - interval '1 month', 'YYYY-MM-DD'));
SELECT create_monthly_range_partitioned_table('history.local_compliance', to_char(current_date - interval '1 month', 'YYYY-MM-DD'));
SELECT create_monthly_range_partitioned_table('event.managed_clusters', to_char(current_date - interval '1 month', 'YYYY-MM-DD'));

