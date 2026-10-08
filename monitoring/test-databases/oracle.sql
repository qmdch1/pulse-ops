-- Isolated Oracle Free test container only. No DBA, catalog role, AWR or ASH access.
ALTER SESSION SET CONTAINER = FREEPDB1;
CREATE USER pulse_observer IDENTIFIED BY "pulse_test_only";
GRANT CREATE SESSION TO pulse_observer;
GRANT SELECT ON SYS.V_$SYSSTAT TO pulse_observer;
GRANT SELECT ON SYS.V_$SESSION TO pulse_observer;
GRANT SELECT ON SYS.V_$RESOURCE_LIMIT TO pulse_observer;
