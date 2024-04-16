# Fleetspeak Database Pruning

## Introduction

If you are running Fleetspeak for a sufficiently long time you will see the
underlying database growing in size. The size of the Fleetspeak database might
become an issue in case you are either running out of space in your database
partition or when you see the performance degrade.

At that point you might decide that it is time to prune the Fleetspeak database.
But... - Where do you start with pruning the Fleetspeak database? - What is a
safe approach to prune the database? - What is best practice?

This README provides you with some guidance on the topic.

## 1. Logging into the Fleetspeak database

Fleetspeak builds on [MySQL](https://dev.mysql.com/doc/refman/8.3/en/) as its
underlying datastore. You can log into your Fleetspeak database with any
compatible [MySQL client](https://dev.mysql.com/doc/refman/8.3/en/mysql.html)
given you have access to the required connection details as well as the
necessary access rights.

```
# The below command assumes that your database is running localy on the standard TCP port 3306, adjust accordingly otherwise.
mysql -h 127.0.0.1 -u MyFleetspeakUser -p
# Enter your password and you should see a greeting like the below followed by the mysql> prompt.
# Welcome to the MySQL monitor.  Commands end with ; or \g.
# Your MySQL connection id is 13
# Server version: 8.2.0 MySQL Community Server - GPL

# make sure you switch to using the Fleetspeak database
mysql> USE fleetspeak
```

## 2. Overview of the Fleetspeak database tables

The Fleetspeak database contains three sets of database tables with one 'main'
table each: - the tables for `clients` information, - the tables for `messages`
information, and - the tables for `broadcasts` information

```
mysql> show tables;
+-------------------------------+
| Tables_in_fleetspeak          |
+-------------------------------+
| broadcast_allocations         |
| broadcast_labels              |
| broadcast_sent                |
| broadcasts                    |
|-------------------------------|
| client_contact_messages       |
| client_contacts               |
| client_labels                 |
| client_resource_usage_records |
| clients                       |
| files                         |
|-------------------------------|
| messages                      |
| pending_messages              |
+-------------------------------+
12 rows in set (0.01 sec)
```

Conveniently the auxiliary tables contain
[ON DELETE CASCADE referential actions](https://dev.mysql.com/doc/refman/8.3/en/create-table-foreign-keys.html#foreign-key-referential-actions)
on the respective
[FOREIGN KEYS](https://dev.mysql.com/doc/refman/8.3/en/create-table-foreign-keys.html).

Therefore, we can focus on pruning rows from the three 'main' tables
(`clients`,`messages` and `broadcasts`) to whip our Fleetspeak database back
into shape. MySQL will ensure that the related tables and their entries will get
cleaned up accordingly.

## 3. The clients table

This table contains records for clients that Fleetspeak communicates with.
Clients with a `last_contact_time` sufficiently far in the past are good
candiates to be pruned.

### 3.1. table details

```
mysql> DESCRIBE clients;
+---------------------------+-----------------+------+-----+---------+-------+
| Field                     | Type            | Null | Key | Default | Extra |
+---------------------------+-----------------+------+-----+---------+-------+
| client_id                 | binary(8)       | NO   | PRI | NULL    |       |
| client_key                | blob            | YES  |     | NULL    |       |
| blacklisted               | tinyint(1)      | NO   |     | NULL    |       |
| last_contact_time         | bigint          | NO   |     | NULL    |       |
| last_contact_address      | text            | YES  |     | NULL    |       |
| last_contact_streaming_to | text            | YES  |     | NULL    |       |
| last_clock_seconds        | bigint unsigned | YES  |     | NULL    |       |
| last_clock_nanos          | int unsigned    | YES  |     | NULL    |       |
+---------------------------+-----------------+------+-----+---------+-------+
8 rows in set (0.00 sec)
```

### 3.2. Pruning clients

With the example `DELETE` command below we delete all the rows that have been
created 168h (7 days) ago or older. Feel free to adjust the time according to
your needs. ``` DELETE FROM clients WHERE FROM_UNIXTIME(last_contact_time /
1000000000) < (NOW() - 3600 * 168);

# For good measures we should also prune any messages that relate to these clients

DELETE FROM messages WHERE source_client_id NOT IN (SELECT client_id FROM
clients) AND destination_client_id NOT IN (SELECT client_id FROM clients); ```

## 4. The messages table

This table contains records with message details that Fleetspeak has exchanged
with clients. Most likely this will be the fastest growing table and well worth
pruning to keep your Fleetspeak database in good shape.

Messages with a `creation_time_seconds` sufficiently far in the past are good
candidates to be pruned.

### 4.1. table details

```
mysql> DESCRIBE messages;
+--------------------------+---------------+------+-----+---------+-------+
| Field                    | Type          | Null | Key | Default | Extra |
+--------------------------+---------------+------+-----+---------+-------+
| message_id               | binary(32)    | NO   | PRI | NULL    |       |
| source_client_id         | binary(8)     | YES  |     | NULL    |       |
| source_service_name      | varchar(128)  | NO   |     | NULL    |       |
| source_message_id        | varbinary(16) | YES  |     | NULL    |       |
| destination_client_id    | binary(8)     | YES  |     | NULL    |       |
| destination_service_name | varchar(128)  | NO   |     | NULL    |       |
| message_type             | varchar(128)  | YES  |     | NULL    |       |
| creation_time_seconds    | bigint        | NO   |     | NULL    |       |
| creation_time_nanos      | int           | NO   |     | NULL    |       |
| processed_time_seconds   | bigint        | YES  |     | NULL    |       |
| processed_time_nanos     | int           | YES  |     | NULL    |       |
| validation_info          | blob          | YES  |     | NULL    |       |
| failed                   | tinyint       | YES  |     | NULL    |       |
| failed_reason            | text          | YES  |     | NULL    |       |
| annotations              | blob          | YES  |     | NULL    |       |
+--------------------------+---------------+------+-----+---------+-------+
15 rows in set (0.01 sec)
```

### 4.2. Pruning messages

With the example `DELETE` command below we delete all the rows that have been
created 168h (7 days) ago or older. Feel free to adjust the time according to
your needs. `DELETE FROM messages WHERE FROM_UNIXTIME(creation_time_seconds) <
(NOW() - 3600 * 168);`

## 5. The broadcasts table

This step is optional and you can safely leave the records in this table in
place as they will not grow to a large size anyway.

### 5.1. table details

```
mysql> DESCRIBE broadcasts;
+-------------------------+-----------------+------+-----+---------+-------+
| Field                   | Type            | Null | Key | Default | Extra |
+-------------------------+-----------------+------+-----+---------+-------+
| broadcast_id            | binary(8)       | NO   | PRI | NULL    |       |
| source_service_name     | varchar(128)    | NO   |     | NULL    |       |
| message_type            | varchar(128)    | NO   |     | NULL    |       |
| expiration_time_seconds | bigint          | YES  |     | NULL    |       |
| expiration_time_nanos   | int             | YES  |     | NULL    |       |
| data_type_url           | text            | YES  |     | NULL    |       |
| data_value              | mediumblob      | YES  |     | NULL    |       |
| sent                    | bigint unsigned | YES  |     | NULL    |       |
| allocated               | bigint unsigned | YES  |     | NULL    |       |
| message_limit           | bigint unsigned | YES  |     | NULL    |       |
+-------------------------+-----------------+------+-----+---------+-------+
10 rows in set (0.00 sec)
```

### 5.2. Pruning broadcasts

As mentioned this table will grow much slower than the tables discussed above.
So you might decide not to prune these entries. However, in case you decide to
prune the `broadcasts` table then the below command is how you can go about it.

With the example `DELETE` command below we delete all the rows that have been
created 168h (7 days) ago or older. Feel free to adjust the time according to
your needs. `DELETE FROM broadcasts WHERE expiration_time_seconds < (NOW() -
3600 * 168);`
