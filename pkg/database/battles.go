package database

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
)

//CreateBattle adds a new battle to the db
func (d *Database) CreateBattle(LeaderID string, BattleName string, PointValuesAllowed []string, Plans []*Plan, AutoFinishVoting bool, PointAverageRounding string) (*Battle, error) {
	var pointValuesJSON, _ = json.Marshal(PointValuesAllowed)

	var b = &Battle{
		BattleID:             "",
		BattleName:           BattleName,
		Users:                make([]*BattleUser, 0),
		Plans:                make([]*Plan, 0),
		VotingLocked:         true,
		ActivePlanID:         "",
		PointValuesAllowed:   PointValuesAllowed,
		AutoFinishVoting:     AutoFinishVoting,
		Leaders:              make([]string, 0),
		PointAverageRounding: "",
	}
	b.Leaders = append(b.Leaders, LeaderID)

	e := d.db.QueryRow(
		`SELECT battleId FROM create_battle($1, $2, $3, $4, $5);`,
		LeaderID,
		BattleName,
		string(pointValuesJSON),
		AutoFinishVoting,
		PointAverageRounding,
	).Scan(&b.BattleID)
	if e != nil {
		log.Println(e)
		return nil, errors.New("error creating battle")
	}

	for _, plan := range Plans {
		plan.Votes = make([]*Vote, 0)

		e := d.db.QueryRow(
			`INSERT INTO plans (battle_id, name, type, reference_id, link, description, acceptance_criteria) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
			b.BattleID,
			plan.PlanName,
			plan.Type,
			plan.ReferenceID,
			plan.Link,
			plan.Description,
			plan.AcceptanceCriteria,
		).Scan(&plan.PlanID)
		if e != nil {
			log.Println(e)
		}
	}

	b.Plans = Plans

	return b, nil
}

// ReviseBattle updates the battle by ID
func (d *Database) ReviseBattle(BattleID string, UserID string, BattleName string, PointValuesAllowed []string, AutoFinishVoting bool, PointAverageRounding string) error {
	err := d.ConfirmLeader(BattleID, UserID)
	if err != nil {
		return errors.New("incorrect permissions")
	}

	var pointValuesJSON, _ = json.Marshal(PointValuesAllowed)
	if _, err := d.db.Exec(
		`UPDATE battles SET name = $2, point_values_allowed = $3, auto_finish_voting = $4, point_average_rounding = $5 WHERE id = $1`, BattleID, BattleName, string(pointValuesJSON), AutoFinishVoting, PointAverageRounding); err != nil {
		log.Println(err)
		return errors.New("unable to revise battle")
	}

	return nil
}

// GetBattle gets a battle by ID
func (d *Database) GetBattle(BattleID string, UserID string) (*Battle, error) {
	var b = &Battle{
		BattleID:             BattleID,
		BattleName:           "",
		Users:                make([]*BattleUser, 0),
		Plans:                make([]*Plan, 0),
		VotingLocked:         true,
		ActivePlanID:         "",
		PointValuesAllowed:   make([]string, 0),
		AutoFinishVoting:     true,
		Leaders:              make([]string, 0),
		PointAverageRounding: "",
	}

	// get battle
	var ActivePlanID sql.NullString
	var pv string
	var leaders string
	e := d.db.QueryRow(
		`
		SELECT b.id, b.name, b.voting_locked, b.active_plan_id, b.point_values_allowed, b.auto_finish_voting, b.point_average_rounding,
		CASE WHEN COUNT(bl) = 0 THEN '[]'::json ELSE array_to_json(array_agg(bl.user_id)) END AS leaders
		FROM battles b
		LEFT JOIN battles_leaders bl ON b.id = bl.battle_id
		WHERE b.id = $1
		GROUP BY b.id`,
		BattleID,
	).Scan(
		&b.BattleID,
		&b.BattleName,
		&b.VotingLocked,
		&ActivePlanID,
		&pv,
		&b.AutoFinishVoting,
		&b.PointAverageRounding,
		&leaders,
	)
	if e != nil {
		log.Println(e)
		return nil, errors.New("not found")
	}

	_ = json.Unmarshal([]byte(pv), &b.PointValuesAllowed)
	_ = json.Unmarshal([]byte(leaders), &b.Leaders)
	b.ActivePlanID = ActivePlanID.String
	b.Users = d.GetBattleUsers(BattleID)
	b.Plans = d.GetPlans(BattleID, UserID)

	return b, nil
}

// GetBattlesByUser gets a list of battles by UserID
func (d *Database) GetBattlesByUser(UserID string) ([]*Battle, error) {
	var battles = make([]*Battle, 0)
	battleRows, battlesErr := d.db.Query(`
		SELECT b.id, b.name, b.voting_locked, b.active_plan_id, b.point_values_allowed, b.auto_finish_voting, b.point_average_rounding,
		CASE WHEN COUNT(p) = 0 THEN '[]'::json ELSE array_to_json(array_agg(row_to_json(p))) END AS plans,
		CASE WHEN COUNT(bl) = 0 THEN '[]'::json ELSE array_to_json(array_agg(bl.user_id)) END AS leaders
		FROM battles b
		LEFT JOIN plans p ON b.id = p.battle_id
		LEFT JOIN battles_leaders bl ON b.id = bl.battle_id
		LEFT JOIN battles_users bw ON b.id = bw.battle_id WHERE bw.user_id = $1 AND bw.abandoned = false
		GROUP BY b.id ORDER BY b.created_date DESC
	`, UserID)
	if battlesErr != nil {
		return nil, errors.New("not found")
	}

	defer battleRows.Close()
	for battleRows.Next() {
		var plans string
		var pv string
		var leaders string
		var ActivePlanID sql.NullString
		var b = &Battle{
			BattleID:             "",
			BattleName:           "",
			Users:                make([]*BattleUser, 0),
			Plans:                make([]*Plan, 0),
			VotingLocked:         true,
			ActivePlanID:         "",
			PointValuesAllowed:   make([]string, 0),
			AutoFinishVoting:     true,
			Leaders:              make([]string, 0),
			PointAverageRounding: "",
		}
		if err := battleRows.Scan(
			&b.BattleID,
			&b.BattleName,
			&b.VotingLocked,
			&ActivePlanID,
			&pv,
			&b.AutoFinishVoting,
			&b.PointAverageRounding,
			&plans,
			&leaders,
		); err != nil {
			log.Println(err)
		} else {
			_ = json.Unmarshal([]byte(plans), &b.Plans)
			_ = json.Unmarshal([]byte(pv), &b.PointValuesAllowed)
			_ = json.Unmarshal([]byte(leaders), &b.Leaders)
			b.ActivePlanID = ActivePlanID.String
			battles = append(battles, b)
		}
	}

	return battles, nil
}

// ConfirmLeader confirms the user is infact leader of the battle
func (d *Database) ConfirmLeader(BattleID string, UserID string) error {
	var leaderID string
	e := d.db.QueryRow("SELECT user_id FROM battles_leaders WHERE battle_id = $1 AND user_id = $2", BattleID, UserID).Scan(&leaderID)
	if e != nil {
		log.Println(e)
		return errors.New("not a battle leader")
	}

	return nil
}

// GetBattleUser gets a user from db by ID and checks battle active status
func (d *Database) GetBattleUser(BattleID string, UserID string) (*BattleUser, error) {
	var active bool
	var w BattleUser

	e := d.db.QueryRow(
		`SELECT
			w.id, w.name, w.type, w.avatar, coalesce(bw.active, FALSE)
		FROM users w
		LEFT JOIN battles_users bw ON bw.user_id = w.id AND bw.battle_id = $1
		WHERE id = $2`,
		BattleID,
		UserID,
	).Scan(
		&w.UserID,
		&w.UserName,
		&w.UserType,
		&w.UserAvatar,
		&active,
	)
	if e != nil {
		log.Println(e)
		return nil, errors.New("user not found")
	}

	if active {
		return nil, errors.New("user already active in battle")
	}

	return &w, nil
}

// GetBattleUsers retrieves the users for a given battle from db
func (d *Database) GetBattleUsers(BattleID string) []*BattleUser {
	var users = make([]*BattleUser, 0)
	rows, err := d.db.Query(
		`SELECT
			w.id, w.name, w.type, w.avatar, bw.active
		FROM battles_users bw
		LEFT JOIN users w ON bw.user_id = w.id
		WHERE bw.battle_id = $1
		ORDER BY w.name`,
		BattleID,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var w BattleUser
			if err := rows.Scan(&w.UserID, &w.UserName, &w.UserType, &w.UserAvatar, &w.Active); err != nil {
				log.Println(err)
			} else {
				users = append(users, &w)
			}
		}
	}

	return users
}

// GetBattleActiveUsers retrieves the active users for a given battle from db
func (d *Database) GetBattleActiveUsers(BattleID string) []*BattleUser {
	var users = make([]*BattleUser, 0)
	rows, err := d.db.Query(
		`SELECT
			w.id, w.name, w.type, w.avatar, bw.active
		FROM battles_users bw
		LEFT JOIN users w ON bw.user_id = w.id
		WHERE bw.battle_id = $1 AND bw.active = true
		ORDER BY w.name`,
		BattleID,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var w BattleUser
			if err := rows.Scan(&w.UserID, &w.UserName, &w.UserType, &w.UserAvatar, &w.Active); err != nil {
				log.Println(err)
			} else {
				users = append(users, &w)
			}
		}
	}

	return users
}

// AddUserToBattle adds a user by ID to the battle by ID
func (d *Database) AddUserToBattle(BattleID string, UserID string) ([]*BattleUser, error) {
	if _, err := d.db.Exec(
		`INSERT INTO battles_users (battle_id, user_id, active)
		VALUES ($1, $2, true)
		ON CONFLICT (battle_id, user_id) DO UPDATE SET active = true, abandoned = false`,
		BattleID,
		UserID,
	); err != nil {
		log.Println(err)
	}

	users := d.GetBattleUsers(BattleID)

	return users, nil
}

// RetreatUser removes a user from the current battle by ID
func (d *Database) RetreatUser(BattleID string, UserID string) []*BattleUser {
	if _, err := d.db.Exec(
		`UPDATE battles_users SET active = false WHERE battle_id = $1 AND user_id = $2`, BattleID, UserID); err != nil {
		log.Println(err)
	}

	if _, err := d.db.Exec(
		`UPDATE users SET last_active = NOW() WHERE id = $1`, UserID); err != nil {
		log.Println(err)
	}

	users := d.GetBattleUsers(BattleID)

	return users
}

// AbandonBattle removes a user from the current battle by ID and sets abandoned true
func (d *Database) AbandonBattle(BattleID string, UserID string) ([]*BattleUser, error) {
	if _, err := d.db.Exec(
		`UPDATE battles_users SET active = false, abandoned = true WHERE battle_id = $1 AND user_id = $2`, BattleID, UserID); err != nil {
		log.Println(err)
		return nil, err
	}

	if _, err := d.db.Exec(
		`UPDATE users SET last_active = NOW() WHERE id = $1`, UserID); err != nil {
		log.Println(err)
		return nil, err
	}

	users := d.GetBattleUsers(BattleID)

	return users, nil
}

// SetBattleLeader sets the leaderId for the battle
func (d *Database) SetBattleLeader(BattleID string, UserID string, LeaderID string) ([]string, error) {
	err := d.ConfirmLeader(BattleID, UserID)
	if err != nil {
		return nil, errors.New("incorrect permissions")
	}

	leaders := make([]string, 0)

	// set battle leader
	if _, err := d.db.Exec(
		`call set_battle_leader($1, $2);`, BattleID, LeaderID); err != nil {
		log.Println(err)
		return nil, errors.New("unable to promote leader")
	}

	leaderRows, leadersErr := d.db.Query(`
		SELECT user_id FROM battles_leaders WHERE battle_id = $1;
	`, BattleID)
	if leadersErr != nil {
		return leaders, nil
	}

	defer leaderRows.Close()
	for leaderRows.Next() {
		var leader string
		if err := leaderRows.Scan(
			&leader,
		); err != nil {
			log.Println(err)
		} else {
			leaders = append(leaders, leader)
		}
	}

	return leaders, nil
}

// DemoteBattleLeader removes a user from battle leaders
func (d *Database) DemoteBattleLeader(BattleID string, UserID string, LeaderID string) ([]string, error) {
	err := d.ConfirmLeader(BattleID, UserID)
	if err != nil {
		return nil, errors.New("incorrect permissions")
	}

	leaders := make([]string, 0)

	// set battle leader
	if _, err := d.db.Exec(
		`call demote_battle_leader($1, $2);`, BattleID, LeaderID); err != nil {
		log.Println(err)
		return nil, errors.New("unable to demote leader")
	}

	leaderRows, leadersErr := d.db.Query(`
		SELECT user_id FROM battles_leaders WHERE battle_id = $1;
	`, BattleID)
	if leadersErr != nil {
		return leaders, nil
	}

	defer leaderRows.Close()
	for leaderRows.Next() {
		var leader string
		if err := leaderRows.Scan(
			&leader,
		); err != nil {
			log.Println(err)
		} else {
			leaders = append(leaders, leader)
		}
	}

	return leaders, nil
}

// DeleteBattle removes all battle associations and the battle itself from DB by BattleID
func (d *Database) DeleteBattle(BattleID string, UserID string) error {
	err := d.ConfirmLeader(BattleID, UserID)
	if err != nil {
		return errors.New("incorrect permissions")
	}

	if _, err := d.db.Exec(
		`call delete_battle($1);`, BattleID); err != nil {
		log.Println(err)
		return err
	}

	return nil
}
