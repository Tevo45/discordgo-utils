package dgutils

import (
	"github.com/bwmarrin/discordgo"
)

/*
 * Blatantly copy-pasted from
 * https://github.com/bwmarrin/discordgo/wiki/FAQ#permissions-and-roles
 */

//
// Checks if a given member with ID userID has permissions permission on guild
// with ID guildID
//
func MemberHasPermissions(s *discordgo.Session, guildID, userID string, permission int) (bool, error) {
	member, err := s.State.Member(guildID, userID)
	if err != nil {
		if member, err = s.GuildMember(guildID, userID); err != nil {
			return false, err
		}
	}

	for _, roleID := range member.Roles {
		role, err := s.State.Role(guildID, roleID)
		if err != nil {
			return false, err
		}
		if role.Permissions&permission != 0 {
			return true, nil
		}
	}

	return false, nil
}

//
// Checks if user with ID userID is owner of guild with ID guildID
//
func IsOwner(s *discordgo.Session, guildID, userID string) (bool, error) {
	guild, err := s.Guild(guildID)
	if err != nil {
		return false, err
	}

	return guild.OwnerID == userID, nil
}
